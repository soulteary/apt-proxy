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
	pathutil "path"
	"strings"
)

// normalizePrefix ensures the prefix is either empty or ends with "/" and never
// has a leading "/". This shape lets us cheaply concatenate it with a key
// without duplicating slashes.
func normalizePrefix(p string) string {
	p = strings.Trim(strings.TrimSpace(p), "/")
	if p == "" {
		return ""
	}
	return p + "/"
}

// toKey converts a vfs-style logical path (e.g. "/body/v1/abcdef") to an S3
// object key by stripping the leading "/" and prepending the configured
// prefix. Trailing slashes are preserved (callers use that to mark directory
// listings).
func (s *S3VFS) toKey(p string) string {
	cleaned := pathutil.Clean("/" + strings.TrimRight(p, "/"))
	if strings.HasSuffix(p, "/") && cleaned != "/" {
		cleaned += "/"
	}
	if cleaned == "/" {
		return s.prefix
	}
	return s.prefix + strings.TrimPrefix(cleaned, "/")
}

// fromKey converts an S3 object key back to its logical vfs path by stripping
// the configured prefix.
func (s *S3VFS) fromKey(key string) string {
	return strings.TrimPrefix(key, s.prefix)
}

// dirPrefix returns the S3 prefix used to list children of the given vfs
// directory. The result always ends with "/" except when path == "/" and no
// configured prefix exists, in which case the bucket root is implied.
func (s *S3VFS) dirPrefix(p string) string {
	cleaned := pathutil.Clean("/" + p)
	if cleaned == "/" {
		return s.prefix
	}
	return s.prefix + strings.TrimPrefix(cleaned, "/") + "/"
}

// baseName returns the final element of a slash-delimited key, stripping a
// trailing slash if present. It mirrors path.Base but works on raw S3 keys.
func baseName(key string) string {
	key = strings.TrimRight(key, "/")
	if i := strings.LastIndex(key, "/"); i >= 0 {
		return key[i+1:]
	}
	return key
}
