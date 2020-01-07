package common

import (
	"fmt"
	"github.com/pkg/errors"
	"io/ioutil"
	"os"
	"path/filepath"
)

func WriteFile(baseDir string, fileName string, body string) {
	if body == "" {
		return
	}

	fn := fmt.Sprintf("%s%c%s", baseDir, filepath.Separator, fileName)
	f, err := os.Create(fn)
	CheckErr(err, "create file failed, f=" + fn)
	defer f.Close()

	if _, err := f.WriteString(body); err != nil {
		CheckErr(err, "write file failed, f=" + fn)
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

func ListAllFiles(path string) []string{
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