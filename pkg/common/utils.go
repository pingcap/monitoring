package common

import (
	"context"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/go-github/v66/github"
	"github.com/pkg/errors"
)

const (
	Monitoring_Owner = "pingcap"
	Monitoirng_Repo  = "monitoring"
	Commit_Message   = "Automatically generate monitoring configurations for %s"
)

var (
	PR_Subject            = "Automatically generate monitoring configurations for %s"
	PR_Description        = "Automatically generate monitoring configurations"
	Monitoring_Base_Brach = "master"
)

func WriteFile(baseDir string, fileName string, body string) {
	if body == "" {
		return
	}

	fn := fmt.Sprintf("%s%c%s", baseDir, filepath.Separator, fileName)
	f, err := os.Create(fn)
	CheckErr(err, "create file failed, f="+fn)
	defer f.Close()

	if _, err := f.WriteString(body); err != nil {
		CheckErr(err, "write file failed, f="+fn)
	}
}

func CheckErr(err error, msg string) {
	if err != nil {
		panic(errors.Wrap(err, msg))
	}
}

func PathExist(path string) bool {
	if _, err := os.Stat(path); err != nil {
		return false
	} else {
		return true
	}
}

func ExtractFromPath(path string) string {
	for i := len(path) - 1; i >= 0; i-- {
		if path[i] == filepath.Separator {
			return path[i:]
		}
	}
	return path
}

func ListAllFiles(path string) []string {
	info, err := os.Stat(path)
	CheckErr(err, "")

	if !info.IsDir() {
		return []string{path}
	}

	return ListFiles(path)
}

func ListFiles(dir string) []string {
	rd, err := ioutil.ReadDir(dir)
	CheckErr(err, "")
	files := make([]string, 0)

	for _, r := range rd {
		path := fmt.Sprintf("%s%c%s", dir, filepath.Separator, r.Name())
		if r.IsDir() {
			paths := ListFiles(path)

			files = append(files, paths...)
		} else {
			files = append(files, path)
		}
	}

	return files
}

func GetRef(client *github.Client, commitBranch string, ctx context.Context) (ref *github.Reference, err error) {
	if ref, _, err = client.Git.GetRef(ctx, Monitoring_Owner, Monitoirng_Repo, "refs/heads/"+commitBranch); err == nil {
		return ref, nil
	}

	// We consider that an error means the branch has not been found and needs to
	// be created.
	if commitBranch == Monitoring_Base_Brach {
		return nil, errors.New("The commit branch does not exist but `-base-branch` is the same as `-commit-branch`")
	}

	var baseRef *github.Reference
	if baseRef, _, err = client.Git.GetRef(ctx, Monitoring_Owner, Monitoirng_Repo, "refs/heads/"+Monitoring_Base_Brach); err != nil {
		return nil, err
	}
	newRef := &github.Reference{Ref: github.String("refs/heads/" + commitBranch), Object: &github.GitObject{SHA: baseRef.Object.SHA}}
	ref, _, err = client.Git.CreateRef(ctx, Monitoring_Owner, Monitoirng_Repo, newRef)
	return ref, err
}

// getTree generates the tree to commit based on the given files and the commit
// of the ref you got in getRef.
func GetTree(client *github.Client, ref *github.Reference, directory string, ctx context.Context, rootDir string) (tree *github.Tree, err error) {
	// Create a tree with what to commit.
	entries := []*github.TreeEntry{}

	// Load each file into the tree.
	files := ListAllFiles(directory)
	for _, fileArg := range files {
		file, content, err := getFileContent(fileArg)
		if err != nil {
			return nil, err
		}

		if rootDir[len(rootDir)-1] == filepath.Separator {
			rootDir = rootDir[0 : len(rootDir)-1]
		}

		treePath := strings.ReplaceAll(file, fmt.Sprintf("%s%c", rootDir, filepath.Separator), "")
		entries = append(entries, &github.TreeEntry{Path: github.String(treePath), Type: github.String("blob"), Content: github.String(string(content)), Mode: github.String("100644")})
	}

	tree, _, err = client.Git.CreateTree(ctx, Monitoring_Owner, Monitoirng_Repo, *ref.Object.SHA, entries)
	return tree, err
}

// getFileContent loads the local content of a file and return the target name
// of the file in the target repository and its contents.
func getFileContent(fileArg string) (targetName string, b []byte, err error) {
	var localFile string
	files := strings.Split(fileArg, ":")
	switch {
	case len(files) < 1:
		return "", nil, errors.New("empty `-files` parameter")
	case len(files) == 1:
		localFile = files[0]
		targetName = files[0]
	default:
		localFile = files[0]
		targetName = files[1]
	}

	b, err = ioutil.ReadFile(localFile)
	return targetName, b, err
}

// createCommit creates the commit in the given reference using the given tree.
func PushCommit(client *github.Client, ref *github.Reference, tree *github.Tree, ctx context.Context, tag string, authorName string, authorEmail string) (err error) {
	// Get the parent commit to attach the commit to.
	parent, _, err := client.Repositories.GetCommit(ctx, Monitoring_Owner, Monitoirng_Repo, *ref.Object.SHA, nil)
	if err != nil {
		return err
	}
	// This is not always populated, but is needed.
	parent.Commit.SHA = parent.SHA

	// Create the commit using the tree.
	date := time.Now()
	author := &github.CommitAuthor{Date: &github.Timestamp{Time: date}, Name: &authorName, Email: &authorEmail}

	commitMsg := fmt.Sprintf(Commit_Message, tag)
	commit := &github.Commit{Author: author, Message: &commitMsg, Tree: tree, Parents: []*github.Commit{parent.Commit}}
	newCommit, _, err := client.Git.CreateCommit(ctx, Monitoring_Owner, Monitoirng_Repo, commit, nil)
	if err != nil {
		return err
	}

	// Attach the commit to the master branch.
	ref.Object.SHA = newCommit.SHA
	_, _, err = client.Git.UpdateRef(ctx, Monitoring_Owner, Monitoirng_Repo, ref, false)
	return err
}

func CreatePR(client *github.Client, commitBranch string, ctx context.Context, tag string) (err error) {
	title := fmt.Sprintf(PR_Subject, tag)
	newPR := &github.NewPullRequest{
		Title:               &title,
		Head:                &commitBranch,
		Base:                &Monitoring_Base_Brach,
		Body:                &PR_Description,
		MaintainerCanModify: github.Bool(true),
	}

	pr, _, err := client.PullRequests.Create(ctx, Monitoring_Owner, Monitoirng_Repo, newPR)
	if err != nil {
		return err
	}

	fmt.Printf("PR created: %s\n", pr.GetHTMLURL())
	return nil
}
