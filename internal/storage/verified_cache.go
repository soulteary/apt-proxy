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

package storage

import (
	"io"
	"strconv"
	"strings"
	"time"

	httpcache "github.com/soulteary/httpcache-kit/v4"
)

// VerifiedCache checks declared body lengths before cache hits are served.
// The built-in disk and VFS caches expose seekable bodies, so this check does
// not read package contents or allocate a package-sized buffer.
type VerifiedCache struct{ httpcache.ExtendedCache }

func (c *VerifiedCache) Retrieve(key string) (*httpcache.Resource, error) {
	res, err := c.ExtendedCache.Retrieve(key)
	if err != nil {
		return nil, err
	}
	// HEAD metadata describes the GET body without carrying that body.
	if !strings.HasPrefix(key, "GET:") {
		return res, nil
	}
	raw := res.Header().Get("Content-Length")
	if raw == "" {
		return res, nil
	}
	expected, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || expected < 0 {
		_ = res.Close()
		return nil, httpcache.ErrNotFoundInCache
	}
	size, err := res.Seek(0, io.SeekEnd)
	if err != nil {
		_ = res.Close()
		return nil, err
	}
	if size != expected {
		_ = res.Close()
		return nil, httpcache.ErrNotFoundInCache
	}
	if _, err = res.Seek(0, io.SeekStart); err != nil {
		_ = res.Close()
		return nil, err
	}
	return res, nil
}

// Preserve persisted invalidation ordering used by the built-in caches.
func (c *VerifiedCache) StaleAt(key string) (time.Time, bool) {
	if cache, ok := c.ExtendedCache.(interface {
		StaleAt(string) (time.Time, bool)
	}); ok {
		return cache.StaleAt(key)
	}
	return time.Time{}, false
}
func (c *VerifiedCache) StaleSnapshot(keys ...string) map[string]time.Time {
	if cache, ok := c.ExtendedCache.(interface {
		StaleSnapshot(...string) map[string]time.Time
	}); ok {
		return cache.StaleSnapshot(keys...)
	}
	result := make(map[string]time.Time)
	for _, key := range keys {
		if at, ok := c.StaleAt(key); ok {
			result[key] = at
		}
	}
	return result
}
