package vfs

import (
	"bytes"
	"compress/gzip"
	"io/ioutil"
	"os"
	"testing"
)

func BenchmarkLoadGoSrc(b *testing.B) {
	f := openOptionalTestFile(b, goTestFile)
	defer f.Close()
	// Decompress to avoid measuring the time to gunzip
	zr, err := gzip.NewReader(f)
	if err != nil {
		b.Fatal(err)
	}
	defer zr.Close()
	data, err := ioutil.ReadAll(zr)
	if err != nil {
		b.Fatal(err)
	}
	b.ResetTimer()
	for ii := 0; ii < b.N; ii++ {
		if _, err := Tar(bytes.NewReader(data)); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkWalkGoSrc(b *testing.B) {
	f := openOptionalTestFile(b, goTestFile)
	defer f.Close()
	fs, err := TarGzip(f)
	if err != nil {
		b.Fatal(err)
	}
	b.ResetTimer()
	for ii := 0; ii < b.N; ii++ {
		Walk(fs, "/", func(_ VFS, _ string, _ os.FileInfo, _ error) error { return nil })
	}
}
