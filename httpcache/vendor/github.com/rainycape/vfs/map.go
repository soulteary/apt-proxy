package vfs

import (
	"path"
	"sort"
)

// Map returns an in-memory file system using the given files argument to
// populate it (which might be nil). Note that the files map does
// not need to contain any directories, they will be created automatically.
// If the files contain conflicting paths (e.g. files named a and a/b, thus
// making "a" both a file and a directory), an error will be returned.
func Map(files map[string]*File) (VFS, error) {
	fs := newMemory()
	keys := make([]string, 0, len(files))
	for k := range files {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var dir *Dir
	var prevDir *Dir
	var prevDirPath string
	for _, k := range keys {
		file := files[k]
		if file.Mode == 0 {
			file.Mode = 0644
		}
		fileDir, fileBase := path.Split(k)
		if prevDir != nil && fileDir == prevDirPath {
			dir = prevDir
		} else {
			if err := MkdirAll(fs, fileDir, 0755); err != nil {
				return nil, err
			}
			var err error
			dir, err = fs.dirEntry(fileDir)
			if err != nil {
				return nil, err
			}
			prevDir = dir
			prevDirPath = fileDir
		}
		if err := dir.Add(fileBase, file); err != nil {
			return nil, err
		}
	}
	return fs, nil
}
