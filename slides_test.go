package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSlideFilesFiltersAndSorts(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"z.JPG", "a.png", "ignore.txt"} {
		if err := os.WriteFile(filepath.Join(dir, name), nil, 0600); err != nil {
			t.Fatal(err)
		}
	}
	files, err := slideFiles(dir)
	if err != nil || len(files) != 2 || filepath.Base(files[0]) != "a.png" || filepath.Base(files[1]) != "z.JPG" {
		t.Fatalf("%v: %v", files, err)
	}
	if _, err := slideFiles("."); err == nil {
		t.Fatal("accepted relative path")
	}
}
