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
	"context"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// memoryS3 runs SDK requests without external services in the short CI suite.
type memoryS3 struct {
	mu           sync.Mutex
	objects      map[string]string
	heads        int
	bucketStatus int
	failUploads  bool
}

func (m *memoryS3) RoundTrip(r *http.Request) (*http.Response, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	status, body := http.StatusOK, ""
	h := make(http.Header)
	h.Set("Last-Modified", "Thu, 01 Jan 2026 00:00:00 GMT")
	h.Set("ETag", `"d41d8cd98f00b204e9800998ecf8427e"`)
	key := strings.TrimPrefix(r.URL.Path, "/test-bucket/")
	switch {
	case r.URL.Query().Has("location"):
		body = `<LocationConstraint xmlns="http://s3.amazonaws.com/doc/2006-03-01/">us-east-1</LocationConstraint>`
	case r.URL.Path == "/test-bucket/" && r.Method == http.MethodHead:
		if m.bucketStatus != 0 {
			status = m.bucketStatus
		}
	case r.URL.Query().Has("list-type"):
		prefix, delimiter := r.URL.Query().Get("prefix"), r.URL.Query().Get("delimiter")
		body = `<ListBucketResult xmlns="http://s3.amazonaws.com/doc/2006-03-01/"><Name>test-bucket</Name><IsTruncated>false</IsTruncated>`
		dirs := map[string]bool{}
		for k, v := range m.objects {
			if !strings.HasPrefix(k, prefix) {
				continue
			}
			tail := strings.TrimPrefix(k, prefix)
			if delimiter != "" && strings.Contains(tail, delimiter) {
				dir := prefix + strings.SplitN(tail, delimiter, 2)[0] + delimiter
				if !dirs[dir] {
					body += "<CommonPrefixes><Prefix>" + dir + "</Prefix></CommonPrefixes>"
					dirs[dir] = true
				}
			} else {
				body += fmt.Sprintf("<Contents><Key>%s</Key><Size>%d</Size><LastModified>2026-01-01T00:00:00Z</LastModified><ETag>\"d41d8cd98f00b204e9800998ecf8427e\"</ETag></Contents>", k, len(v))
			}
		}
		body += "</ListBucketResult>"
	case r.Method == http.MethodPost && r.URL.Query().Has("delete"):
		var request struct {
			Objects []struct {
				Key string `xml:"Key"`
			} `xml:"Object"`
		}
		if err := xml.NewDecoder(r.Body).Decode(&request); err != nil {
			return nil, err
		}
		body = "<DeleteResult>"
		for _, object := range request.Objects {
			delete(m.objects, object.Key)
			body += "<Deleted><Key>" + object.Key + "</Key></Deleted>"
		}
		body += "</DeleteResult>"
	case r.Method == http.MethodPut:
		if m.failUploads {
			status = 403
			body = "<Error><Code>AccessDenied</Code></Error>"
			break
		}
		data, err := io.ReadAll(r.Body)
		if err != nil {
			return nil, err
		}
		m.objects[key] = string(data)
	case r.Method == http.MethodDelete:
		delete(m.objects, key)
		status = 204
	case r.Method == http.MethodHead || r.Method == http.MethodGet:
		value, ok := m.objects[key]
		if !ok {
			status = 404
			body = "<Error><Code>NoSuchKey</Code></Error>"
			break
		}
		h.Set("Content-Length", strconv.Itoa(len(value)))
		if r.Method == http.MethodHead {
			m.heads++
		} else {
			body = value
			if rng := r.Header.Get("Range"); rng != "" {
				var start, end int
				if _, err := fmt.Sscanf(rng, "bytes=%d-%d", &start, &end); err != nil {
					end = len(value) - 1
					_, _ = fmt.Sscanf(rng, "bytes=%d-", &start)
				}
				if end >= len(value) {
					end = len(value) - 1
				}
				body = value[start : end+1]
				status = 206
				h.Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, end, len(value)))
				h.Set("Content-Length", strconv.Itoa(len(body)))
			}
		}
	default:
		return nil, fmt.Errorf("unexpected S3 request: %s %s", r.Method, r.URL)
	}
	return &http.Response{StatusCode: status, Header: h, Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
}

func newMemoryVFS(t *testing.T, threshold int64) (*S3VFS, *memoryS3) {
	t.Helper()
	backend := &memoryS3{objects: map[string]string{}}
	fs, err := New(context.Background(), Config{Endpoint: "s3.test", Region: "us-east-1", Bucket: "test-bucket", Prefix: "cache", UsePathStyle: true, InlineMaxBytes: threshold, TempDir: t.TempDir(), HTTPClient: &http.Client{Transport: backend}})
	require.NoError(t, err)
	return fs, backend
}

func TestS3CacheLifecycle(t *testing.T) {
	for _, threshold := range []int64{1024, 4} {
		t.Run(strconv.FormatInt(threshold, 10), func(t *testing.T) {
			fs, backend := newMemoryVFS(t, threshold)
			require.Equal(t, "test-bucket", fs.Bucket())
			require.Contains(t, fs.String(), "cache")
			require.NoError(t, fs.HealthCheck(context.Background()))
			require.NoError(t, fs.Mkdir("packages", 0755))
			root, err := fs.Stat("/")
			require.NoError(t, err)
			require.True(t, root.IsDir())
			require.Equal(t, os.ModeDir|0755, root.Mode())
			_, err = fs.Open("/")
			require.ErrorIs(t, err, os.ErrInvalid)
			_, err = fs.OpenFile("/", os.O_CREATE, 0644)
			require.ErrorIs(t, err, os.ErrInvalid)
			_, err = fs.Open("missing")
			require.ErrorIs(t, err, os.ErrNotExist)
			w, err := fs.OpenFile("packages/test.deb", os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
			require.NoError(t, err)
			_, err = w.Write([]byte("hello "))
			require.NoError(t, err)
			_, err = w.Write([]byte("world"))
			require.NoError(t, err)
			require.NoError(t, w.Close())
			require.NoError(t, w.Close())
			entries, err := os.ReadDir(fs.tmpDir)
			require.NoError(t, err)
			require.Empty(t, entries)
			info, err := fs.Lstat("packages/test.deb")
			require.NoError(t, err)
			require.Equal(t, "test.deb", info.Name())
			require.EqualValues(t, 11, info.Size())
			require.Equal(t, os.FileMode(0644), info.Mode())
			require.False(t, info.IsDir())
			require.Nil(t, info.Sys())
			require.False(t, info.ModTime().IsZero())
			heads := backend.heads
			_, err = fs.Stat("packages/test.deb")
			require.NoError(t, err)
			require.Equal(t, heads, backend.heads)
			fs.metaCacheTTL = time.Nanosecond
			_, err = fs.Stat("packages/test.deb")
			require.NoError(t, err)
			require.Greater(t, backend.heads, heads)
			rf, err := fs.Open("packages/test.deb")
			require.NoError(t, err)
			data, err := io.ReadAll(rf)
			require.NoError(t, err)
			require.Equal(t, "hello world", string(data))
			_, err = rf.Seek(6, io.SeekStart)
			require.NoError(t, err)
			data, err = io.ReadAll(rf)
			require.NoError(t, err)
			require.Equal(t, "world", string(data))
			at, ok := rf.(io.ReaderAt)
			require.True(t, ok)
			buf := make([]byte, 5)
			_, err = at.ReadAt(buf, 0)
			require.NoError(t, err)
			require.Equal(t, "hello", string(buf))
			require.NoError(t, rf.Close())
			ro, err := fs.OpenFile("packages/test.deb", os.O_RDONLY, 0)
			require.NoError(t, err)
			_, err = ro.Write([]byte("bad"))
			require.ErrorIs(t, err, errReadOnWrite)
			_, err = ro.Seek(0, io.SeekStart)
			require.NoError(t, err)
			data, err = io.ReadAll(ro)
			require.NoError(t, err)
			require.Equal(t, "hello world", string(data))
			require.NoError(t, ro.Close())
			dir, err := fs.Stat("packages")
			require.NoError(t, err)
			require.True(t, dir.IsDir())
			listing, err := fs.ReadDir("/")
			require.NoError(t, err)
			require.Len(t, listing, 1)
			require.True(t, listing[0].IsDir())
			listing, err = fs.ReadDir("packages")
			require.NoError(t, err)
			require.Len(t, listing, 1)
			require.Equal(t, "test.deb", listing[0].Name())
			require.NoError(t, fs.Remove("packages/test.deb"))
			_, err = fs.Stat("packages/test.deb")
			require.ErrorIs(t, err, os.ErrNotExist)
			for _, key := range []string{"packages/a", "packages/nested/b"} {
				w, err = fs.OpenFile(key, os.O_CREATE|os.O_WRONLY, 0644)
				require.NoError(t, err)
				_, err = w.Write([]byte("data"))
				require.NoError(t, err)
				require.NoError(t, w.Close())
			}
			require.NoError(t, fs.Remove("packages/"))
			require.Empty(t, backend.objects)
			fs.prefix = ""
			require.ErrorIs(t, fs.Remove("/"), os.ErrInvalid)
		})
	}
}

func TestS3UploadFailureCleansSpillAndInvalidatesMetadata(t *testing.T) {
	fs, backend := newMemoryVFS(t, 4)
	backend.objects["cache/file"] = "old"
	_, err := fs.Stat("file")
	require.NoError(t, err)
	backend.failUploads = true
	w, err := fs.OpenFile("file", os.O_CREATE|os.O_WRONLY, 0644)
	require.NoError(t, err)
	_, err = w.Write([]byte("replacement"))
	require.NoError(t, err)
	require.Error(t, w.Close())
	require.False(t, fs.metaCache.Contains("cache/file"))
	entries, err := os.ReadDir(fs.tmpDir)
	require.NoError(t, err)
	require.Empty(t, entries)
	backend.bucketStatus = 404
	require.Error(t, fs.HealthCheck(context.Background()))
}
