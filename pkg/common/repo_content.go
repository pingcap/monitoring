package common

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"net/url"
	"reflect"
	"strings"

	"golang.org/x/oauth2"

	"github.com/google/go-querystring/query"
)

const (
	headerOTP      = "X-GitHub-OTP"
	defaultBaseURL = "https://api.github.com/"
	defaultOwner   = "pingcap"
)

// RepositoryContent represents a file or directory in a github repository.
type RepositoryContent struct {
	Type     *string `json:"type,omitempty"`
	Encoding *string `json:"encoding,omitempty"`
	Size     *int    `json:"size,omitempty"`
	Name     *string `json:"name,omitempty"`
	Path     *string `json:"path,omitempty"`
	// Content contains the actual file content, which may be encoded.
	// Callers should call GetContent which will decode the content if
	// necessary.
	Content     *string `json:"content,omitempty"`
	SHA         *string `json:"sha,omitempty"`
	URL         *string `json:"url,omitempty"`
	GitURL      *string `json:"git_url,omitempty"`
	HTMLURL     *string `json:"html_url,omitempty"`
	DownloadURL *string `json:"download_url,omitempty"`
}

// RepositoryContentFileOptions specifies optional parameters for CreateFile, UpdateFile, and DeleteFile.
type RepositoryContentFileOptions struct {
	Message *string `json:"message,omitempty"`
	Content []byte  `json:"content,omitempty"` // unencoded
	SHA     *string `json:"sha,omitempty"`
	Branch  *string `json:"branch,omitempty"`
}

// RepositoryContentGetOptions represents an optional ref parameter, which can be a SHA,
// branch, or tag
type RepositoryContentGetOptions struct {
	Ref string `url:"ref,omitempty"`
}

type GitRepoService struct {
	baseURL *url.URL
	client  *http.Client
}

func NewGitRepoService() (*GitRepoService, error) {
	url, err := url.Parse(defaultBaseURL)
	if err != nil {
		return nil, err
	}

	return &GitRepoService{
		baseURL: url,
		client:  http.DefaultClient,
	}, nil
}

func NewGitRepoServiceWithAuth(auth BasicAuthTransport) (*GitRepoService, error) {
	url, err := url.Parse(defaultBaseURL)
	if err != nil {
		return nil, err
	}

	return &GitRepoService{
		baseURL: url,
		client:  auth.Client(),
	}, nil
}

func NewGitRepoServiceWithToken(token string) (*GitRepoService, error) {
	url, err := url.Parse(defaultBaseURL)
	if err != nil {
		return nil, err
	}

	ctx := context.Background()
	ts := oauth2.StaticTokenSource(
		&oauth2.Token{AccessToken: token})
	tc := oauth2.NewClient(ctx, ts)

	return &GitRepoService{
		baseURL: url,
		client:  tc,
	}, nil
}

// BasicAuthTransport is an http.RoundTripper that authenticates all requests
// using HTTP Basic Authentication with the provided username and password. It
// additionally supports users who have two-factor authentication enabled on
// their GitHub account.
type BasicAuthTransport struct {
	Username string // GitHub username
	Password string // GitHub password
	OTP      string // one-time password for users with two-factor auth enabled

	// Transport is the underlying HTTP transport to use when making requests.
	// It will default to http.DefaultTransport if nil.
	Transport http.RoundTripper
}

// RoundTrip implements the RoundTripper interface.
func (t *BasicAuthTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	// To set extra headers, we must make a copy of the Request so
	// that we don't modify the Request we were given. This is required by the
	// specification of http.RoundTripper.
	//
	// Since we are going to modify only req.Header here, we only need a deep copy
	// of req.Header.
	req2 := new(http.Request)
	*req2 = *req
	req2.Header = make(http.Header, len(req.Header))
	for k, s := range req.Header {
		req2.Header[k] = append([]string(nil), s...)
	}

	req2.SetBasicAuth(t.Username, t.Password)
	if t.OTP != "" {
		req2.Header.Set(headerOTP, t.OTP)
	}
	return t.transport().RoundTrip(req2)
}

// Client returns an *http.Client that makes requests that are authenticated
// using HTTP Basic Authentication.
func (t *BasicAuthTransport) Client() *http.Client {
	return &http.Client{Transport: t}
}

func (t *BasicAuthTransport) transport() http.RoundTripper {
	if t.Transport != nil {
		return t.Transport
	}
	return http.DefaultTransport
}

// GitHub API docs: https://developer.github.com/v3/repos/contents/#get-contents
func (r *GitRepoService) GetContents(owner, repo, path string, opt *RepositoryContentGetOptions) (fileContent *RepositoryContent, directoryContent []*RepositoryContent, err error) {
	escapedPath := (&url.URL{Path: path}).String()
	if owner == "" {
		owner = defaultOwner
	}

	u := fmt.Sprintf("repos/%s/%s/contents/%s", owner, repo, escapedPath)
	u, err = addOptions(u, opt)
	if err != nil {
		return nil, nil, err
	}
	req, err := r.NewRequest("GET", u, nil)
	if err != nil {
		return nil, nil, err
	}

	resp, err := r.client.Do(req)
	if err != nil {
		return nil, nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, nil, errors.New(fmt.Sprintf("Bad request, code=%d, status=%s", resp.StatusCode, resp.Status))
	}

	var rawJSON json.RawMessage

	decErr := json.NewDecoder(resp.Body).Decode(&rawJSON)
	if decErr == io.EOF {
		decErr = nil // ignore EOF errors caused by empty response body
	}
	if decErr != nil {
		return nil, nil, decErr
	}

	fileUnmarshalError := json.Unmarshal(rawJSON, &fileContent)
	if fileUnmarshalError == nil {
		return fileContent, nil, nil
	}

	directoryUnmarshalError := json.Unmarshal(rawJSON, &directoryContent)
	if directoryUnmarshalError == nil {
		return nil, directoryContent, nil
	}
	return nil, nil, fmt.Errorf("unmarshalling failed for both file and directory content: %s and %s", fileUnmarshalError, directoryUnmarshalError)
}

// addOptions adds the parameters in opt as URL query parameters to s. opt
// must be a struct whose fields may contain "url" tags.
func addOptions(s string, opt interface{}) (string, error) {
	v := reflect.ValueOf(opt)
	if v.Kind() == reflect.Ptr && v.IsNil() {
		return s, nil
	}

	u, err := url.Parse(s)
	if err != nil {
		return s, err
	}

	qs, err := query.Values(opt)
	if err != nil {
		return s, err
	}

	u.RawQuery = qs.Encode()
	return u.String(), nil
}

// NewRequest creates an API request. A relative URL can be provided in urlStr,
// in which case it is resolved relative to the BaseURL of the Client.
// Relative URLs should always be specified without a preceding slash. If
// specified, the value pointed to by body is JSON encoded and included as the
// request body.
func (c *GitRepoService) NewRequest(method, urlStr string, body interface{}) (*http.Request, error) {
	if !strings.HasSuffix(c.baseURL.Path, "/") {
		return nil, fmt.Errorf("BaseURL must have a trailing slash, but %q does not", c.baseURL)
	}
	u, err := c.baseURL.Parse(urlStr)
	if err != nil {
		return nil, err
	}

	var buf io.ReadWriter
	if body != nil {
		buf = new(bytes.Buffer)
		enc := json.NewEncoder(buf)
		enc.SetEscapeHTML(false)
		err := enc.Encode(body)
		if err != nil {
			return nil, err
		}
	}

	req, err := http.NewRequest(method, u.String(), buf)
	if err != nil {
		return nil, err
	}

	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("Accept", "application/vnd.github.v3+json")
	return req, nil
}

func (s *GitRepoService) DownloadContents(content *RepositoryContent) ([]byte, error) {
	if content.DownloadURL == nil || *content.DownloadURL == "" {
		fmt.Println(fmt.Sprintf("No download link found for %s", *content.Name))
		return nil, nil
	}

	resp, err := s.client.Get(*content.DownloadURL)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return nil, errors.New(fmt.Sprintf("fetch content failed for %s", *content.Name))
	}

	return ioutil.ReadAll(resp.Body)
}
