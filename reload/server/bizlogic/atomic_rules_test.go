package bizlogic

import (
	"io"
	"os"
	"path/filepath"
	"testing"
)

func TestRuleReplacementIsAtomic(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "rules.yml")
	if err := os.WriteFile(path, []byte("old complete rules"), 0600); err != nil {
		t.Fatal(err)
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	old, err := root.Open("rules.yml")
	if err != nil {
		t.Fatal(err)
	}
	defer old.Close()
	if err := writeRegularConfig(root, "rules.yml", []byte("new complete rules"), 0); err != nil {
		t.Fatal(err)
	}
	before, err := io.ReadAll(old)
	if err != nil || string(before) != "old complete rules" {
		t.Fatalf("existing reader observed in-place overwrite: %q %v", before, err)
	}
	after, err := root.ReadFile("rules.yml")
	if err != nil || string(after) != "new complete rules" {
		t.Fatalf("new reader sees incomplete data: %q %v", after, err)
	}
	info, err := root.Stat("rules.yml")
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatal("replacement loosened private file permissions", err)
	}
	files, err := os.ReadDir(dir)
	if err != nil || len(files) != 1 {
		t.Fatal("temporary file leaked", err)
	}
}
