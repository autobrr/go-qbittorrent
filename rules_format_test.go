package qbittorrent

import (
	"bytes"
	"go/format"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
)

func TestGoFilesAreGofmted(t *testing.T) {
	err := filepath.WalkDir(".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if path != "." && (d.Name() == "testdata" || d.Name()[0] == '.') {
				return filepath.SkipDir
			}
			return nil
		}
		if filepath.Ext(path) != ".go" {
			return nil
		}
		src, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		formatted, err := format.Source(src)
		if err != nil {
			t.Errorf("%s: %v", path, err)
			return nil
		}
		if !bytes.Equal(src, formatted) {
			t.Errorf("%s is not gofmt-formatted: run gofmt -w %s, or go generate ./... for a generated file", path, path)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
