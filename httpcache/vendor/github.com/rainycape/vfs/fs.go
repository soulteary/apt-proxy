package vfs

import (
	"fmt"
	"io/ioutil"
	"os"
	"path"
	"path/filepath"
)

// IMPORTANT: Note about wrapping os. functions: os.Open, os.OpenFile etc... will return a non-nil
// interface pointing to a nil instance in case of error (whoever decided this disctintion in Go
// was a good idea deservers to be hung by his thumbs). This is highly undesirable, since users
// can't rely on checking f != nil to know if a correct handle was returned. That's why the
// methods in fileSystem do the error checking themselves and return a true nil in case of error.

type fileSystem struct {
	root      string
	temporary bool
}

func (fs *fileSystem) path(name string) string {
	name = path.Clean("/" + name)
	return filepath.Join(fs.root, filepath.FromSlash(name))
}

// Root returns the root directory of the fileSystem, as an
// absolute path native to the current operating system.
func (fs *fileSystem) Root() string {
	return fs.root
}

// IsTemporary returns wheter the fileSystem is temporary.
func (fs *fileSystem) IsTemporary() bool {
	return fs.temporary
}

func (fs *fileSystem) Open(path string) (RFile, error) {
	f, err := os.Open(fs.path(path))
	if err != nil {
		return nil, err
	}
	return f, nil
}

func (fs *fileSystem) OpenFile(path string, flag int, mode os.FileMode) (WFile, error) {
	f, err := os.OpenFile(fs.path(path), flag, mode)
	if err != nil {
		return nil, err
	}
	return f, nil
}

func (fs *fileSystem) Lstat(path string) (os.FileInfo, error) {
	info, err := os.Lstat(fs.path(path))
	if err != nil {
		return nil, err
	}
	return info, nil
}

func (fs *fileSystem) Stat(path string) (os.FileInfo, error) {
	info, err := os.Stat(fs.path(path))
	if err != nil {
		return nil, err
	}
	return info, nil
}

func (fs *fileSystem) ReadDir(path string) ([]os.FileInfo, error) {
	files, err := ioutil.ReadDir(fs.path(path))
	if err != nil {
		return nil, err
	}
	return files, nil
}

func (fs *fileSystem) Mkdir(path string, perm os.FileMode) error {
	return os.Mkdir(fs.path(path), perm)
}

func (fs *fileSystem) Remove(path string) error {
	return os.Remove(fs.path(path))
}

func (fs *fileSystem) String() string {
	return fmt.Sprintf("fileSystem: %s", fs.root)
}

// Close is a no-op on non-temporary filesystems. On temporary
// ones (as returned by TmpFS), it removes all the temporary files.
func (f *fileSystem) Close() error {
	if f.temporary {
		return os.RemoveAll(f.root)
	}
	return nil
}

func newFS(root string) (*fileSystem, error) {
	abs, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	return &fileSystem{root: abs}, nil
}

// FS returns a VFS at the given path, which must be provided
// as native path of the current operating system. The path might be
// either absolute or relative, but the fileSystem will be anchored
// at the absolute path represented by root at the time of the function
// call.
func FS(root string) (VFS, error) {
	return newFS(root)
}

// TmpFS returns a temporary file system with the given prefix and its root
// directory name, which might be empty. The temporary file system is created
// in the default temporary directory for the operating system. Once you're
// done with the temporary filesystem, you might can all its files by calling
// its Close method.
func TmpFS(prefix string) (TemporaryVFS, error) {
	dir, err := ioutil.TempDir("", prefix)
	if err != nil {
		return nil, err
	}
	fs, err := newFS(dir)
	if err != nil {
		return nil, err
	}
	fs.temporary = true
	return fs, nil
}
