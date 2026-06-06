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

//go:build !unix

package system

import "errors"

// DiskAvailable returns the number of bytes available. On non-Unix platforms
// it returns an error so callers can show "N/A" or equivalent. The path
// argument is accepted for API parity with the Unix implementation.
func DiskAvailable(path ...string) (uint64, error) {
	_ = path
	return 0, errors.New("disk available not supported on this platform")
}
