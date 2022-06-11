package vfs

import (
	"fmt"
	"os"
)

type rewriterFileSystem struct {
	fs       VFS
	rewriter func(string) string
}

func (fs *rewriterFileSystem) VFS() VFS {
	return fs.fs
}

func (fs *rewriterFileSystem) Open(path string) (RFile, error) {
	return fs.fs.Open(fs.rewriter(path))
}

func (fs *rewriterFileSystem) OpenFile(path string, flag int, perm os.FileMode) (WFile, error) {
	return fs.fs.OpenFile(fs.rewriter(path), flag, perm)
}

func (fs *rewriterFileSystem) Lstat(path string) (os.FileInfo, error) {
	return fs.fs.Lstat(fs.rewriter(path))
}

func (fs *rewriterFileSystem) Stat(path string) (os.FileInfo, error) {
	return fs.fs.Stat(fs.rewriter(path))
}

func (fs *rewriterFileSystem) ReadDir(path string) ([]os.FileInfo, error) {
	return fs.fs.ReadDir(fs.rewriter(path))
}

func (fs *rewriterFileSystem) Mkdir(path string, perm os.FileMode) error {
	return fs.fs.Mkdir(fs.rewriter(path), perm)
}

func (fs *rewriterFileSystem) Remove(path string) error {
	return fs.fs.Remove(fs.rewriter(path))
}

func (fs *rewriterFileSystem) String() string {
	return fmt.Sprintf("Rewriter %s", fs.fs.String())
}

// Rewriter returns a file system which uses the provided function
// to rewrite paths.
func Rewriter(fs VFS, rewriter func(oldPath string) (newPath string)) VFS {
	if rewriter == nil {
		return fs
	}
	return &rewriterFileSystem{fs: fs, rewriter: rewriter}
}
