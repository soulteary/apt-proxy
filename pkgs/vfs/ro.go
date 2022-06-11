package vfs

import (
	"errors"
	"fmt"
	"os"
)

var (
	// ErrReadOnlyFileSystem is the error returned by read only file systems
	// from calls which would result in a write operation.
	ErrReadOnlyFileSystem = errors.New("read-only filesystem")
)

type readOnlyFileSystem struct {
	fs VFS
}

func (fs *readOnlyFileSystem) VFS() VFS {
	return fs.fs
}

func (fs *readOnlyFileSystem) Open(path string) (RFile, error) {
	return fs.fs.Open(path)
}

func (fs *readOnlyFileSystem) OpenFile(path string, flag int, perm os.FileMode) (WFile, error) {
	if flag&(os.O_CREATE|os.O_WRONLY|os.O_RDWR) != 0 {
		return nil, ErrReadOnlyFileSystem
	}
	return fs.fs.OpenFile(path, flag, perm)
}

func (fs *readOnlyFileSystem) Lstat(path string) (os.FileInfo, error) {
	return fs.fs.Lstat(path)
}

func (fs *readOnlyFileSystem) Stat(path string) (os.FileInfo, error) {
	return fs.fs.Stat(path)
}

func (fs *readOnlyFileSystem) ReadDir(path string) ([]os.FileInfo, error) {
	return fs.fs.ReadDir(path)
}

func (fs *readOnlyFileSystem) Mkdir(path string, perm os.FileMode) error {
	return ErrReadOnlyFileSystem
}

func (fs *readOnlyFileSystem) Remove(path string) error {
	return ErrReadOnlyFileSystem
}

func (fs *readOnlyFileSystem) String() string {
	return fmt.Sprintf("RO %s", fs.fs.String())
}

// ReadOnly returns a read-only filesystem wrapping the given fs.
func ReadOnly(fs VFS) VFS {
	return &readOnlyFileSystem{fs: fs}
}
