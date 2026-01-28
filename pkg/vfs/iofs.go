package vfs

import (
	"io"
	"io/fs"
	"path"
	"path/filepath"
	"strings"
)

// AsReadOnlyFS returns a read-only fs.FS view of the given VFS.
// It allows read-only code paths to use fs.ReadFile, fs.WalkDir, fs.Glob, etc.
// Names use forward slashes; "." represents the root. The VFS root is "/" or ".".
// The returned value implements fs.ReadFileFS so fs.ReadFile uses Open+ReadAll
// directly and avoids the default read loop.
func AsReadOnlyFS(v VFS) fs.FS {
	return &readOnlyFSAdapter{v: v}
}

// ReadFile implements fs.ReadFileFS for read-only paths.
func (a *readOnlyFSAdapter) ReadFile(name string) ([]byte, error) {
	name = path.Clean(name)
	if name == "." {
		name = "/"
	}
	if strings.Contains(name, "..") {
		return nil, &fs.PathError{Op: "readfile", Path: name, Err: ErrInvalidPath}
	}
	return ReadFile(a.v, name)
}

type readOnlyFSAdapter struct {
	v VFS
}

// Open opens the named file. name must use forward slashes; "." is the root.
func (a *readOnlyFSAdapter) Open(name string) (fs.File, error) {
	name = path.Clean(name)
	if name == "." {
		name = "/"
	}
	if strings.Contains(name, "..") {
		return nil, &fs.PathError{Op: "open", Path: name, Err: ErrInvalidPath}
	}
	info, err := a.v.Stat(name)
	if err != nil {
		return nil, err
	}
	if info.IsDir() {
		return &adapterDirFile{v: a.v, path: name, info: info}, nil
	}
	r, err := a.v.Open(name)
	if err != nil {
		return nil, err
	}
	return &adapterFileFile{r: r, info: info}, nil
}

// adapterFileFile implements fs.File for a regular file (read from VFS).
type adapterFileFile struct {
	r    RFile
	info fs.FileInfo
}

func (f *adapterFileFile) Stat() (fs.FileInfo, error) { return f.info, nil }
func (f *adapterFileFile) Read(p []byte) (int, error) { return f.r.Read(p) }
func (f *adapterFileFile) Close() error               { return f.r.Close() }

// adapterDirFile implements fs.File and fs.ReadDirFile for a directory.
// When n > 0, ReadDir returns at most n entries per call and uses cached
// results so that repeated calls return the next batch (io/fs contract).
type adapterDirFile struct {
	v       VFS
	path    string
	info    fs.FileInfo
	cached  []fs.DirEntry // populated on first ReadDir
	nextIdx int           // next index to return when n > 0
}

func (f *adapterDirFile) Stat() (fs.FileInfo, error) { return f.info, nil }
func (f *adapterDirFile) Read([]byte) (int, error)   { return 0, nil } // dirs are not readable
func (f *adapterDirFile) Close() error               { return nil }

func (f *adapterDirFile) ReadDir(n int) ([]fs.DirEntry, error) {
	if f.cached == nil {
		infos, err := f.v.ReadDir(f.path)
		if err != nil {
			return nil, err
		}
		f.cached = make([]fs.DirEntry, 0, len(infos))
		for _, i := range infos {
			f.cached = append(f.cached, fs.FileInfoToDirEntry(i))
		}
	}
	if n <= 0 {
		return f.cached[f.nextIdx:], nil
	}
	if f.nextIdx >= len(f.cached) {
		return nil, io.EOF // at end of directory, per io/fs contract
	}
	end := f.nextIdx + n
	if end > len(f.cached) {
		end = len(f.cached)
	}
	out := f.cached[f.nextIdx:end]
	f.nextIdx = end
	return out, nil
}

// VFSPathFromFSName converts an io/fs name (forward slashes, "." = root) to
// the path form used by this VFS (e.g. "/" for root). Useful when wrapping
// an fs.FS that was created with AsReadOnlyFS.
func VFSPathFromFSName(name string) string {
	name = path.Clean(name)
	if name == "." || name == "" {
		return "/"
	}
	return filepath.FromSlash(name)
}
