package vfs

import (
	"errors"
	"fmt"
	"os"
	pathpkg "path"
	"strings"
	"sync"
	"time"
)

var (
	errNoEmptyNameFile = errors.New("can't create file with empty name")
	errNoEmptyNameDir  = errors.New("can't create directory with empty name")
)

type memoryFileSystem struct {
	mu   sync.RWMutex
	root *Dir
}

// entry must always be called with the lock held
func (fs *memoryFileSystem) entry(path string) (Entry, *Dir, int, error) {
	path = cleanPath(path)
	if path == "" || path == "/" || path == "." {
		return fs.root, nil, 0, nil
	}
	if path[0] == '/' {
		path = path[1:]
	}
	dir := fs.root
	for {
		p := strings.IndexByte(path, '/')
		name := path
		if p > 0 {
			name = path[:p]
			path = path[p+1:]
		} else {
			path = ""
		}
		dir.RLock()
		entry, pos, err := dir.Find(name)
		dir.RUnlock()
		if err != nil {
			return nil, nil, 0, err
		}
		if len(path) == 0 {
			return entry, dir, pos, nil
		}
		if entry.Type() != EntryTypeDir {
			break
		}
		dir = entry.(*Dir)
	}
	return nil, nil, 0, os.ErrNotExist
}

func (fs *memoryFileSystem) dirEntry(path string) (*Dir, error) {
	entry, _, _, err := fs.entry(path)
	if err != nil {
		return nil, err
	}
	if entry.Type() != EntryTypeDir {
		return nil, fmt.Errorf("%s it's not a directory", path)
	}
	return entry.(*Dir), nil
}

func (fs *memoryFileSystem) Open(path string) (RFile, error) {
	entry, _, _, err := fs.entry(path)
	if err != nil {
		return nil, err
	}
	if entry.Type() != EntryTypeFile {
		return nil, fmt.Errorf("%s is not a file", path)
	}
	return NewRFile(entry.(*File))
}

func (fs *memoryFileSystem) OpenFile(path string, flag int, mode os.FileMode) (WFile, error) {
	if mode&os.ModeType != 0 {
		return nil, fmt.Errorf("%T does not support special files", fs)
	}
	path = cleanPath(path)
	dir, base := pathpkg.Split(path)
	if base == "" {
		return nil, errNoEmptyNameFile
	}
	fs.mu.RLock()
	d, err := fs.dirEntry(dir)
	fs.mu.RUnlock()
	if err != nil {
		return nil, err
	}

	d.Lock()
	defer d.Unlock()
	f, _, _ := d.Find(base)
	if f == nil && flag&os.O_CREATE == 0 {
		return nil, os.ErrNotExist
	}
	// Read only file?
	if flag&os.O_WRONLY == 0 && flag&os.O_RDWR == 0 {
		if f == nil {
			return nil, os.ErrNotExist
		}
		return NewWFile(f.(*File), true, false)
	}
	// Write file, either f != nil or flag&os.O_CREATE
	if f != nil {
		if f.Type() != EntryTypeFile {
			return nil, fmt.Errorf("%s is not a file", path)
		}
		if flag&os.O_EXCL != 0 {
			return nil, os.ErrExist
		}
		// Check if we should truncate
		if flag&os.O_TRUNC != 0 {
			file := f.(*File)
			file.Lock()
			file.ModTime = time.Now()
			file.Data = nil
			file.Unlock()
		}
	} else {
		f = &File{ModTime: time.Now()}
		d.Add(base, f)
	}
	return NewWFile(f.(*File), flag&os.O_RDWR != 0, true)
}

func (fs *memoryFileSystem) Lstat(path string) (os.FileInfo, error) {
	return fs.Stat(path)
}

func (fs *memoryFileSystem) Stat(path string) (os.FileInfo, error) {
	entry, _, _, err := fs.entry(path)
	if err != nil {
		return nil, err
	}
	return &EntryInfo{Path: path, Entry: entry}, nil
}

func (fs *memoryFileSystem) ReadDir(path string) ([]os.FileInfo, error) {
	fs.mu.RLock()
	defer fs.mu.RUnlock()
	return fs.readDir(path)
}

func (fs *memoryFileSystem) readDir(path string) ([]os.FileInfo, error) {
	entry, _, _, err := fs.entry(path)
	if err != nil {
		return nil, err
	}
	if entry.Type() != EntryTypeDir {
		return nil, fmt.Errorf("%s is not a directory", path)
	}
	dir := entry.(*Dir)
	dir.RLock()
	infos := make([]os.FileInfo, len(dir.Entries))
	for ii, v := range dir.EntryNames {
		infos[ii] = &EntryInfo{
			Path:  pathpkg.Join(path, v),
			Entry: dir.Entries[ii],
		}
	}
	dir.RUnlock()
	return infos, nil
}

func (fs *memoryFileSystem) Mkdir(path string, perm os.FileMode) error {
	path = cleanPath(path)
	dir, base := pathpkg.Split(path)
	if base == "" {
		if dir == "/" || dir == "" {
			return os.ErrExist
		}
		return errNoEmptyNameDir
	}
	fs.mu.RLock()
	d, err := fs.dirEntry(dir)
	fs.mu.RUnlock()
	if err != nil {
		return err
	}
	d.Lock()
	defer d.Unlock()
	if _, p, _ := d.Find(base); p >= 0 {
		return os.ErrExist
	}
	d.Add(base, &Dir{
		Mode:    os.ModeDir | perm,
		ModTime: time.Now(),
	})
	return nil
}

func (fs *memoryFileSystem) Remove(path string) error {
	entry, dir, pos, err := fs.entry(path)
	if err != nil {
		return err
	}
	if entry.Type() == EntryTypeDir && len(entry.(*Dir).Entries) > 0 {
		return fmt.Errorf("directory %s not empty", path)
	}
	// Lock again, the position might have changed
	dir.Lock()
	_, pos, err = dir.Find(pathpkg.Base(path))
	if err == nil {
		dir.EntryNames = append(dir.EntryNames[:pos], dir.EntryNames[pos+1:]...)
		dir.Entries = append(dir.Entries[:pos], dir.Entries[pos+1:]...)
	}
	dir.Unlock()
	return err
}

func (fs *memoryFileSystem) String() string {
	return "MemoryFileSystem"
}

func newMemory() *memoryFileSystem {
	fs := &memoryFileSystem{
		root: &Dir{
			Mode:    os.ModeDir | 0755,
			ModTime: time.Now(),
		},
	}
	return fs
}

// Memory returns an empty in memory VFS.
func Memory() VFS {
	return newMemory()
}

func cleanPath(path string) string {
	return strings.Trim(pathpkg.Clean("/"+path), "/")
}
