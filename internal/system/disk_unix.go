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

//go:build unix

package system

import (
	"os"

	"golang.org/x/sys/unix"
)

// DiskAvailable returns the number of bytes available to the current user
// in the filesystem hosting the given path. When path is empty it falls
// back to the current working directory (legacy behaviour). Unix-only.
func DiskAvailable(path ...string) (uint64, error) {
	target := ""
	if len(path) > 0 && path[0] != "" {
		target = path[0]
	}
	if target == "" {
		wd, err := os.Getwd()
		if err != nil {
			return 0, err
		}
		target = wd
	}
	var stat unix.Statfs_t
	if err := unix.Statfs(target, &stat); err != nil {
		return 0, err
	}
	if stat.Bsize <= 0 {
		return 0, nil
	}
	return stat.Bavail * uint64(stat.Bsize), nil
}
