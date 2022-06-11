package vfs

import (
	"path/filepath"
	"testing"
)

func testOpenedVFS(t *testing.T, fs VFS) {
	data1, err := ReadFile(fs, "a/b/c/d")
	if err != nil {
		t.Fatal(err)
	}
	if string(data1) != "go" {
		t.Errorf("expecting a/b/c/d to contain \"go\", it contains %q instead", string(data1))
	}
	data2, err := ReadFile(fs, "empty")
	if err != nil {
		t.Fatal(err)
	}
	if len(data2) > 0 {
		t.Error("non-empty empty file")
	}
}

func testOpenFilename(t *testing.T, filename string) {
	p := filepath.Join("testdata", filename)
	fs, err := Open(p)
	if err != nil {
		t.Fatal(err)
	}
	testOpenedVFS(t, fs)
}

func TestOpenZip(t *testing.T) {
	testOpenFilename(t, "fs.zip")
}

func TestOpenTar(t *testing.T) {
	testOpenFilename(t, "fs.tar")
}

func TestOpenTarGzip(t *testing.T) {
	testOpenFilename(t, "fs.tar.gz")
}

func TestOpenTarBzip2(t *testing.T) {
	testOpenFilename(t, "fs.tar.bz2")
}
