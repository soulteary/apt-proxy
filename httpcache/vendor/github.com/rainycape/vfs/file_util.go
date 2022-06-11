package vfs

import (
	"bytes"
	"compress/zlib"
	"errors"
	"fmt"
	"io"
	"os"
	"runtime"
	"time"
)

var (
	errFileClosed = errors.New("file is closed")
)

// NewRFile returns a RFile from a *File.
func NewRFile(f *File) (RFile, error) {
	data, err := fileData(f)
	if err != nil {
		return nil, err
	}
	return &file{f: f, data: data, readable: true}, nil
}

// NewWFile returns a WFile from a *File.
func NewWFile(f *File, read bool, write bool) (WFile, error) {
	data, err := fileData(f)
	if err != nil {
		return nil, err
	}
	w := &file{f: f, data: data, readable: read, writable: write}
	runtime.SetFinalizer(w, closeFile)
	return w, nil
}

func closeFile(f *file) {
	f.Close()
}

func fileData(f *File) ([]byte, error) {
	if len(f.Data) == 0 || f.Mode&ModeCompress == 0 {
		return f.Data, nil
	}
	zr, err := zlib.NewReader(bytes.NewReader(f.Data))
	if err != nil {
		return nil, err
	}
	defer zr.Close()
	var out bytes.Buffer
	if _, err := io.Copy(&out, zr); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}

type file struct {
	f        *File
	data     []byte
	offset   int
	readable bool
	writable bool
	closed   bool
}

func (f *file) Read(p []byte) (int, error) {
	if !f.readable {
		return 0, ErrWriteOnly
	}
	f.f.RLock()
	defer f.f.RUnlock()
	if f.closed {
		return 0, errFileClosed
	}
	if f.offset > len(f.data) {
		return 0, io.EOF
	}
	n := copy(p, f.data[f.offset:])
	f.offset += n
	if n < len(p) {
		return n, io.EOF
	}
	return n, nil
}

func (f *file) Seek(offset int64, whence int) (int64, error) {
	f.f.Lock()
	defer f.f.Unlock()
	if f.closed {
		return 0, errFileClosed
	}
	switch whence {
	case os.SEEK_SET:
		f.offset = int(offset)
	case os.SEEK_CUR:
		f.offset += int(offset)
	case os.SEEK_END:
		f.offset = len(f.data) + int(offset)
	default:
		panic(fmt.Errorf("Seek: invalid whence %d", whence))
	}
	if f.offset > len(f.data) {
		f.offset = len(f.data)
	} else if f.offset < 0 {
		f.offset = 0
	}
	return int64(f.offset), nil
}

func (f *file) Write(p []byte) (int, error) {
	if !f.writable {
		return 0, ErrReadOnly
	}
	f.f.Lock()
	defer f.f.Unlock()
	if f.closed {
		return 0, errFileClosed
	}
	count := len(p)
	n := copy(f.data[f.offset:], p)
	if n < count {
		f.data = append(f.data, p[n:]...)
	}
	f.offset += count
	f.f.ModTime = time.Now()
	return count, nil
}

func (f *file) Close() error {
	if !f.closed {
		f.f.Lock()
		defer f.f.Unlock()
		if !f.closed {
			if f.f.Mode&ModeCompress != 0 {
				var buf bytes.Buffer
				zw := zlib.NewWriter(&buf)
				if _, err := zw.Write(f.data); err != nil {
					return err
				}
				if err := zw.Close(); err != nil {
					return err
				}
				if buf.Len() < len(f.data) {
					f.f.Data = buf.Bytes()
				} else {
					f.f.Mode &= ^ModeCompress
					f.f.Data = f.data
				}
			} else {
				f.f.Data = f.data
			}
			f.closed = true
		}
	}
	return nil
}

func (f *file) IsCompressed() bool {
	return f.f.Mode&ModeCompress != 0
}

func (f *file) SetCompressed(c bool) {
	f.f.Lock()
	defer f.f.Unlock()
	if c {
		f.f.Mode |= ModeCompress
	} else {
		f.f.Mode &= ^ModeCompress
	}
}
