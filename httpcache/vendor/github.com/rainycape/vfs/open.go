package vfs

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/bzip2"
	"compress/gzip"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"
)

// Zip returns an in-memory VFS initialized with the
// contents of the .zip file read from the given io.Reader.
// Since archive/zip requires an io.ReaderAt rather than an
// io.Reader, and a known size, Zip will read the whole file
// into memory and provide its own buffering if r does not
// implement io.ReaderAt or size is <= 0.
func Zip(r io.Reader, size int64) (VFS, error) {
	rat, _ := r.(io.ReaderAt)
	if rat == nil || size <= 0 {
		data, err := ioutil.ReadAll(r)
		if err != nil {
			return nil, err
		}
		rat = bytes.NewReader(data)
		size = int64(len(data))
	}
	zr, err := zip.NewReader(rat, size)
	if err != nil {
		return nil, err
	}
	files := make(map[string]*File)
	for _, file := range zr.File {
		if file.Mode().IsDir() {
			continue
		}
		f, err := file.Open()
		if err != nil {
			return nil, err
		}
		data, err := ioutil.ReadAll(f)
		f.Close()
		if err != nil {
			return nil, err
		}
		files[file.Name] = &File{
			Data:    data,
			Mode:    file.Mode(),
			ModTime: file.ModTime(),
		}
	}
	return Map(files)
}

// Tar returns an in-memory VFS initialized with the
// contents of the .tar file read from the given io.Reader.
func Tar(r io.Reader) (VFS, error) {
	files := make(map[string]*File)
	tr := tar.NewReader(r)
	for {
		hdr, err := tr.Next()
		if err != nil {
			if err == io.EOF {
				break
			}
			return nil, err
		}
		if hdr.FileInfo().IsDir() {
			continue
		}
		data, err := ioutil.ReadAll(tr)
		if err != nil {
			return nil, err
		}
		files[hdr.Name] = &File{
			Data:    data,
			Mode:    hdr.FileInfo().Mode(),
			ModTime: hdr.ModTime,
		}
	}
	return Map(files)
}

// TarGzip returns an in-memory VFS initialized with the
// contents of the .tar.gz file read from the given io.Reader.
func TarGzip(r io.Reader) (VFS, error) {
	zr, err := gzip.NewReader(r)
	if err != nil {
		return nil, err
	}
	defer zr.Close()
	return Tar(zr)
}

// TarBzip2 returns an in-memory VFS initialized with the
// contents of then .tar.bz2 file read from the given io.Reader.
func TarBzip2(r io.Reader) (VFS, error) {
	bzr := bzip2.NewReader(r)
	return Tar(bzr)
}

// Open returns an in-memory VFS initialized with the contents
// of the given filename, which must have one of the following
// extensions:
//
//  - .zip
//  - .tar
//  - .tar.gz
//  - .tar.bz2
func Open(filename string) (VFS, error) {
	f, err := os.Open(filename)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	base := filepath.Base(filename)
	ext := strings.ToLower(filepath.Ext(base))
	nonExt := filename[:len(filename)-len(ext)]
	if strings.ToLower(filepath.Ext(nonExt)) == ".tar" {
		ext = ".tar" + ext
	}
	switch ext {
	case ".zip":
		st, err := f.Stat()
		if err != nil {
			return nil, err
		}
		return Zip(f, st.Size())
	case ".tar":
		return Tar(f)
	case ".tar.gz":
		return TarGzip(f)
	case ".tar.bz2":
		return TarBzip2(f)
	}
	return nil, fmt.Errorf("can't open a VFS from a %s file", ext)
}
