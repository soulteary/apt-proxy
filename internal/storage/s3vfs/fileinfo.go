// Copyright 2022 Su Yang
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package s3vfs

import (
	"os"
	"time"
)

// s3FileInfo is the os.FileInfo adapter for S3 objects and synthetic
// directories. httpcache-kit only inspects Name(), Size(), ModTime() and
// IsDir(); the other methods return reasonable defaults so that any future
// caller using io/fs.WalkDir, etc., still works.
type s3FileInfo struct {
	name    string
	size    int64
	modTime time.Time
	isDir   bool
}

func (i *s3FileInfo) Name() string { return i.name }
func (i *s3FileInfo) Size() int64  { return i.size }

// Mode returns 0755|os.ModeDir for directories and 0644 for objects. S3 has
// no real permission concept; we mirror the values used by vfs.Memory().
func (i *s3FileInfo) Mode() os.FileMode {
	if i.isDir {
		return os.FileMode(0o755) | os.ModeDir
	}
	return 0o644
}

func (i *s3FileInfo) ModTime() time.Time { return i.modTime }
func (i *s3FileInfo) IsDir() bool        { return i.isDir }
func (i *s3FileInfo) Sys() any           { return nil }
