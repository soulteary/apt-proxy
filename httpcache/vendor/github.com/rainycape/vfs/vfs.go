package vfs

import (
	"io"
	"os"
)

// Opener is the interface which specifies the methods for
// opening a file. All the VFS implementations implement
// this interface.
type Opener interface {
	// Open returns a readable file at the given path. See also
	// the shorthand function ReadFile.
	Open(path string) (RFile, error)
	// OpenFile returns a readable and writable file at the given
	// path. Note that, depending on the flags, the file might be
	// only readable or only writable. See also the shorthand
	// function WriteFile.
	OpenFile(path string, flag int, perm os.FileMode) (WFile, error)
}

// RFile is the interface implemented by the returned value from a VFS
// Open method. It allows reading and seeking, and must be closed after use.
type RFile interface {
	io.Reader
	io.Seeker
	io.Closer
}

// WFile is the interface implemented by the returned value from a VFS
// OpenFile method. It allows reading, seeking and writing, and must
// be closed after use. Note that, depending on the flags passed to
// OpenFile, the Read or Write methods might always return an error (e.g.
// if the file was opened in read-only or write-only mode).
type WFile interface {
	io.Reader
	io.Writer
	io.Seeker
	io.Closer
}

// VFS is the interface implemented by all the Virtual File Systems.
type VFS interface {
	Opener
	// Lstat returns the os.FileInfo for the given path, without
	// following symlinks.
	Lstat(path string) (os.FileInfo, error)
	// Stat returns the os.FileInfo for the given path, following
	// symlinks.
	Stat(path string) (os.FileInfo, error)
	// ReadDir returns the contents of the directory at path as an slice
	// of os.FileInfo, ordered alphabetically by name. If path is not a
	// directory or the permissions don't allow it, an error will be
	// returned.
	ReadDir(path string) ([]os.FileInfo, error)
	// Mkdir creates a directory at the given path. If the directory
	// already exists or its parent directory does not exist or
	// the permissions don't allow it, an error will be returned. See
	// also the shorthand function MkdirAll.
	Mkdir(path string, perm os.FileMode) error
	// Remove removes the item at the given path. If the path does
	// not exists or the permissions don't allow removing it or it's
	// a non-empty directory, an error will be returned. See also
	// the shorthand function RemoveAll.
	Remove(path string) error
	// String returns a human-readable description of the VFS.
	String() string
}

// TemporaryVFS represents a temporary on-disk file system which can be removed
// by calling its Close method.
type TemporaryVFS interface {
	VFS
	// Root returns the root directory for the temporary VFS.
	Root() string
	// Close removes all the files in temporary VFS.
	Close() error
}

// Container is implemented by some file systems which
// contain another one.
type Container interface {
	// VFS returns the underlying VFS.
	VFS() VFS
}
