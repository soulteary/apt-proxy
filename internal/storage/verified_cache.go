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
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	httpcache "github.com/soulteary/httpcache-kit/v4"
)

// VerifiedCache checks declared body lengths before cache hits are served.
// The built-in disk and VFS caches expose seekable bodies, so this check does
// not read package contents or allocate a package-sized buffer.
type VerifiedCache struct{ httpcache.ExtendedCache }

// A new namespace prevents anonymous reuse of legacy authenticated responses
// whose stored headers do not reveal their request credentials. Existing files
// remain available to normal backend cleanup instead of being deleted on boot.
const privateCacheNamespace = "apt-proxy-private-v1:"

// Varied keys have a versioned namespace before the HTTP method. Derive their
// GET prefix from the public key encoder so length checks do not depend on the
// library's private namespace spelling. Plain and legacy GET keys stay valid.
var variedGETCacheKeyPrefix = func() string {
	req := &http.Request{Method: http.MethodGet, URL: &url.URL{}}
	key := httpcache.NewRequestKey(req).Vary("Accept-Language", req).String()
	prefix, _, _ := strings.Cut(key, ":")
	return prefix + ":"
}()

func privateCacheKeys(keys []string) []string {
	result := make([]string, len(keys))
	for i, key := range keys {
		result[i] = privateCacheNamespace + key
	}
	return result
}
func (c *VerifiedCache) Header(key string) (httpcache.Header, error) {
	return c.ExtendedCache.Header(privateCacheNamespace + key)
}
func (c *VerifiedCache) Store(res *httpcache.Resource, keys ...string) error {
	return c.ExtendedCache.Store(res, privateCacheKeys(keys)...)
}
func (c *VerifiedCache) Invalidate(keys ...string) {
	c.ExtendedCache.Invalidate(privateCacheKeys(keys)...)
}
func (c *VerifiedCache) Freshen(res *httpcache.Resource, keys ...string) error {
	return c.ExtendedCache.Freshen(res, privateCacheKeys(keys)...)
}

func (c *VerifiedCache) Retrieve(key string) (*httpcache.Resource, error) {
	return c.retrieve(key, false)
}

// RetrieveForRecheck preserves verification and ordering while suppressing
// repeated native cache counts during coordinated fill retries.
func (c *VerifiedCache) RetrieveForRecheck(key string) (*httpcache.Resource, error) {
	return c.retrieve(key, true)
}

func (c *VerifiedCache) retrieve(key string, recheck bool) (*httpcache.Resource, error) {
	retrieve := c.ExtendedCache.Retrieve
	if recheck {
		if cache, ok := c.ExtendedCache.(interface {
			RetrieveForRecheck(string) (*httpcache.Resource, error)
		}); ok {
			retrieve = cache.RetrieveForRecheck
		}
	}
	res, err := retrieve(privateCacheNamespace + key)
	if err != nil {
		return nil, err
	}
	// Do not replay legacy session responses created before cookie isolation.
	if len(res.Header().Values("Set-Cookie")) > 0 {
		_ = res.Close()
		return nil, httpcache.ErrNotFoundInCache
	}
	// HEAD metadata describes the GET body without carrying that body.
	if !strings.HasPrefix(key, "GET:") && !strings.HasPrefix(key, variedGETCacheKeyPrefix) {
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
		return cache.StaleAt(privateCacheNamespace + key)
	}
	return time.Time{}, false
}
func (c *VerifiedCache) StaleSnapshot(keys ...string) map[string]time.Time {
	if cache, ok := c.ExtendedCache.(interface {
		StaleSnapshot(...string) map[string]time.Time
	}); ok {
		snapshot := cache.StaleSnapshot(privateCacheKeys(keys)...)
		result := make(map[string]time.Time, len(snapshot))
		for _, key := range keys {
			if at, ok := snapshot[privateCacheNamespace+key]; ok {
				result[key] = at
			}
		}
		return result
	}
	result := make(map[string]time.Time)
	for _, key := range keys {
		if at, ok := c.StaleAt(key); ok {
			result[key] = at
		}
	}
	return result
}
