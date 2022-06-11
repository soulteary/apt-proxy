package vfs

import (
	"fmt"
	"os"
	"path"
	"strings"
)

const (
	separator = "/"
)

func hasSubdir(root, dir string) (string, bool) {
	root = path.Clean(root)
	if !strings.HasSuffix(root, separator) {
		root += separator
	}
	dir = path.Clean(dir)
	if !strings.HasPrefix(dir, root) {
		return "", false
	}
	return dir[len(root):], true
}

type mountPoint struct {
	point string
	fs    VFS
}

func (m *mountPoint) String() string {
	return fmt.Sprintf("%s at %s", m.fs, m.point)
}

// Mounter implements the VFS interface and allows mounting different virtual
// file systems at arbitraty points, working much like a UNIX filesystem.
// Note that the first mounted filesystem must be always at "/".
type Mounter struct {
	points []*mountPoint
}

func (m *Mounter) fs(p string) (VFS, string, error) {
	for ii := len(m.points) - 1; ii >= 0; ii-- {
		if rel, ok := hasSubdir(m.points[ii].point, p); ok {
			return m.points[ii].fs, rel, nil
		}
	}
	return nil, "", os.ErrNotExist
}

// Mount mounts the given filesystem at the given mount point. Unless the
// mount point is /, it must be an already existing directory.
func (m *Mounter) Mount(fs VFS, point string) error {
	point = path.Clean(point)
	if point == "." || point == "" {
		point = "/"
	}
	if point == "/" {
		if len(m.points) > 0 {
			return fmt.Errorf("%s is already mounted at /", m.points[0])
		}
		m.points = append(m.points, &mountPoint{point, fs})
		return nil
	}
	stat, err := m.Stat(point)
	if err != nil {
		return err
	}
	if !stat.IsDir() {
		return fmt.Errorf("%s is not a directory", point)
	}
	m.points = append(m.points, &mountPoint{point, fs})
	return nil
}

// Umount umounts the filesystem from the given mount point. If there are other filesystems
// mounted below it or there's no filesystem mounted at that point, an error is returned.
func (m *Mounter) Umount(point string) error {
	point = path.Clean(point)
	for ii, v := range m.points {
		if v.point == point {
			// Check if we have mount points below this one
			for _, vv := range m.points[ii:] {
				if _, ok := hasSubdir(v.point, vv.point); ok {
					return fmt.Errorf("can't umount %s because %s is mounted below it", point, vv)
				}
			}
			m.points = append(m.points[:ii], m.points[ii+1:]...)
			return nil
		}
	}
	return fmt.Errorf("no filesystem mounted at %s", point)
}

func (m *Mounter) Open(path string) (RFile, error) {
	fs, p, err := m.fs(path)
	if err != nil {
		return nil, err
	}
	return fs.Open(p)
}

func (m *Mounter) OpenFile(path string, flag int, perm os.FileMode) (WFile, error) {
	fs, p, err := m.fs(path)
	if err != nil {
		return nil, err
	}
	return fs.OpenFile(p, flag, perm)
}

func (m *Mounter) Lstat(path string) (os.FileInfo, error) {
	fs, p, err := m.fs(path)
	if err != nil {
		return nil, err
	}
	return fs.Lstat(p)
}

func (m *Mounter) Stat(path string) (os.FileInfo, error) {
	fs, p, err := m.fs(path)
	if err != nil {
		return nil, err
	}
	return fs.Stat(p)
}

func (m *Mounter) ReadDir(path string) ([]os.FileInfo, error) {
	fs, p, err := m.fs(path)
	if err != nil {
		return nil, err
	}
	return fs.ReadDir(p)
}

func (m *Mounter) Mkdir(path string, perm os.FileMode) error {
	fs, p, err := m.fs(path)
	if err != nil {
		return err
	}
	return fs.Mkdir(p, perm)
}

func (m *Mounter) Remove(path string) error {
	// TODO: Don't allow removing an empty directory
	// with a mount below it.
	fs, p, err := m.fs(path)
	if err != nil {
		return err
	}
	return fs.Remove(p)
}

func (m *Mounter) String() string {
	s := make([]string, len(m.points))
	for ii, v := range m.points {
		s[ii] = v.String()
	}
	return fmt.Sprintf("Mounter: %s", strings.Join(s, ", "))
}

func mounterCompileTimeCheck() VFS {
	return &Mounter{}
}
