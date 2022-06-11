package vfs

import (
	"bytes"
	"io"
	"path/filepath"
	"testing"
)

type writeTester struct {
	name   string
	writer func(io.Writer, VFS) error
	reader func(io.Reader) (VFS, error)
}

func TestWrite(t *testing.T) {
	var (
		writeTests = []writeTester{
			{"zip", WriteZip, func(r io.Reader) (VFS, error) { return Zip(r, 0) }},
			{"tar", WriteTar, Tar},
			{"tar.gz", WriteTarGzip, TarGzip},
		}
	)
	p := filepath.Join("testdata", "fs.zip")
	fs, err := Open(p)
	if err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	for _, v := range writeTests {
		buf.Reset()
		if err := v.writer(&buf, fs); err != nil {
			t.Fatalf("error writing %s: %s", v.name, err)
		}
		newFs, err := v.reader(&buf)
		if err != nil {
			t.Fatalf("error reading %s: %s", v.name, err)
		}
		testOpenedVFS(t, newFs)
	}
}
