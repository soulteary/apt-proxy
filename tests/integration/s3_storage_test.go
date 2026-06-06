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

//go:build integration

// Package integration's S3-storage tests run against a live MinIO instance.
//
// They are gated by APT_PROXY_TEST_S3_ENDPOINT (and friends) so that the
// default `go test -tags=integration ./...` invocation skips them on
// developer workstations without docker. The CI workflow under
// .github/workflows/ci.yaml provisions a minio service container and exports
// the env vars; see also examples/s3-minio/ for a local docker-compose setup.
package integration

import (
	"bytes"
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"

	"github.com/soulteary/apt-proxy/internal/storage/s3vfs"
	httpcache "github.com/soulteary/httpcache-kit"
)

// s3TestEnv collects the MinIO connection details. We check via env vars so
// the test binary stays usable both locally (developer-supplied MinIO) and in
// CI (service-container supplied).
type s3TestEnv struct {
	endpoint     string
	accessKey    string
	secretKey    string
	bucket       string
	useSSL       bool
	usePathStyle bool
}

// loadS3TestEnv reads the standard APT_PROXY_TEST_S3_* env vars and returns
// them, or skips the test when the endpoint is unset. The S3 tests demand a
// live server; there is no point pretending otherwise.
func loadS3TestEnv(t *testing.T) s3TestEnv {
	t.Helper()
	env := s3TestEnv{
		endpoint:     os.Getenv("APT_PROXY_TEST_S3_ENDPOINT"),
		accessKey:    envOrDefault("APT_PROXY_TEST_S3_ACCESS_KEY", "minioadmin"),
		secretKey:    envOrDefault("APT_PROXY_TEST_S3_SECRET_KEY", "minioadmin"),
		bucket:       envOrDefault("APT_PROXY_TEST_S3_BUCKET", "apt-proxy-itest"),
		useSSL:       os.Getenv("APT_PROXY_TEST_S3_USE_SSL") == "true",
		usePathStyle: envOrDefault("APT_PROXY_TEST_S3_USE_PATH_STYLE", "true") == "true",
	}
	if env.endpoint == "" {
		t.Skip("APT_PROXY_TEST_S3_ENDPOINT not set; skipping S3 integration test")
	}
	return env
}

func envOrDefault(name, def string) string {
	if v := os.Getenv(name); v != "" {
		return v
	}
	return def
}

// ensureBucket lazily creates the integration bucket. We don't fail when the
// bucket already exists; that is the common case in CI where a fixture step
// pre-provisions it.
func ensureBucket(ctx context.Context, t *testing.T, env s3TestEnv) {
	t.Helper()
	cli, err := minio.New(env.endpoint, &minio.Options{
		Creds:        credentials.NewStaticV4(env.accessKey, env.secretKey, ""),
		Secure:       env.useSSL,
		BucketLookup: lookup(env.usePathStyle),
	})
	if err != nil {
		t.Fatalf("minio.New: %v", err)
	}
	exists, err := cli.BucketExists(ctx, env.bucket)
	if err != nil {
		t.Fatalf("BucketExists: %v", err)
	}
	if !exists {
		if err := cli.MakeBucket(ctx, env.bucket, minio.MakeBucketOptions{}); err != nil {
			t.Fatalf("MakeBucket: %v", err)
		}
	}
}

func lookup(pathStyle bool) minio.BucketLookupType {
	if pathStyle {
		return minio.BucketLookupPath
	}
	return minio.BucketLookupAuto
}

// TestS3VFSEndToEnd runs the full Open / Stat / ReadDir / Remove / WFile path
// against a real MinIO server. It uses a per-test prefix so reruns are
// idempotent and parallel test runs don't collide.
func TestS3VFSEndToEnd(t *testing.T) {
	env := loadS3TestEnv(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	ensureBucket(ctx, t, env)

	prefix := fmt.Sprintf("test-%d/", time.Now().UnixNano())
	fs, err := s3vfs.New(ctx, s3vfs.Config{
		Endpoint:     env.endpoint,
		Bucket:       env.bucket,
		Prefix:       prefix,
		AccessKey:    env.accessKey,
		SecretKey:    env.secretKey,
		UseSSL:       env.useSSL,
		UsePathStyle: env.usePathStyle,
	})
	if err != nil {
		t.Fatalf("s3vfs.New: %v", err)
	}

	// Round-trip a small object through OpenFile + Open. This is the most
	// common path: tiny header files (a few KB) that should never spill.
	const path = "body/v1/abcdef"
	const payload = "hello world"
	wf, err := fs.OpenFile(path, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatalf("OpenFile: %v", err)
	}
	if _, err := wf.Write([]byte(payload)); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if err := wf.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	rf, err := fs.Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	got, err := io.ReadAll(rf)
	_ = rf.Close()
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	if string(got) != payload {
		t.Errorf("round-trip body = %q, want %q", got, payload)
	}

	// Stat & ReadDir
	if info, err := fs.Stat(path); err != nil {
		t.Errorf("Stat: %v", err)
	} else if info.Size() != int64(len(payload)) {
		t.Errorf("Stat size = %d, want %d", info.Size(), len(payload))
	}

	infos, err := fs.ReadDir("body/v1")
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	found := false
	for _, fi := range infos {
		if fi.Name() == "abcdef" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("ReadDir missing freshly-written file; entries=%v", infos)
	}

	// Remove + Stat-not-found
	if err := fs.Remove(path); err != nil {
		t.Errorf("Remove: %v", err)
	}
	if _, err := fs.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("Stat after Remove: got %v, want os.ErrNotExist", err)
	}
}

// TestS3VFSLargeWriteSpills exercises the spill-to-temp-file path with a
// payload larger than InlineMaxBytes. The body must round-trip byte-for-byte
// even after going through disk staging.
func TestS3VFSLargeWriteSpills(t *testing.T) {
	env := loadS3TestEnv(t)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	ensureBucket(ctx, t, env)

	prefix := fmt.Sprintf("test-large-%d/", time.Now().UnixNano())
	fs, err := s3vfs.New(ctx, s3vfs.Config{
		Endpoint:       env.endpoint,
		Bucket:         env.bucket,
		Prefix:         prefix,
		AccessKey:      env.accessKey,
		SecretKey:      env.secretKey,
		UseSSL:         env.useSSL,
		UsePathStyle:   env.usePathStyle,
		InlineMaxBytes: 1024, // tiny so any non-trivial write spills
	})
	if err != nil {
		t.Fatalf("s3vfs.New: %v", err)
	}

	payload := make([]byte, 32*1024) // 32 KiB > 1 KiB threshold
	if _, err := rand.Read(payload); err != nil {
		t.Fatalf("rand.Read: %v", err)
	}

	wf, err := fs.OpenFile("body/v1/large", os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatalf("OpenFile: %v", err)
	}
	if _, err := wf.Write(payload); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if err := wf.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	rf, err := fs.Open("body/v1/large")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	got, err := io.ReadAll(rf)
	_ = rf.Close()
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	if !bytes.Equal(got, payload) {
		t.Errorf("large-payload mismatch: got %d bytes, want %d", len(got), len(payload))
	}
}

// TestS3VFSCacheRoundTrip uses the actual httpcache-kit Store/Retrieve API
// against the S3 backend. This is the highest-fidelity test: a regression in
// either s3vfs or how httpcache-kit drives a vfs.VFS will surface here.
func TestS3VFSCacheRoundTrip(t *testing.T) {
	env := loadS3TestEnv(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	ensureBucket(ctx, t, env)

	prefix := fmt.Sprintf("test-cache-%d/", time.Now().UnixNano())
	fs, err := s3vfs.New(ctx, s3vfs.Config{
		Endpoint:     env.endpoint,
		Bucket:       env.bucket,
		Prefix:       prefix,
		AccessKey:    env.accessKey,
		SecretKey:    env.secretKey,
		UseSSL:       env.useSSL,
		UsePathStyle: env.usePathStyle,
	})
	if err != nil {
		t.Fatalf("s3vfs.New: %v", err)
	}

	c := httpcache.NewVFSCacheWithConfig(fs, nil)
	defer func() { _ = c.Close() }()

	const key = "GET https://example.com/dist/Packages.gz"
	header := http.Header{}
	header.Set("Content-Type", "application/octet-stream")
	header.Set("Content-Length", "12")
	res := httpcache.NewResourceBytes(http.StatusOK, []byte("packagesdata"), header)

	if err := c.Store(res, key); err != nil {
		t.Fatalf("Store: %v", err)
	}

	out, err := c.Retrieve(key)
	if err != nil {
		t.Fatalf("Retrieve: %v", err)
	}
	gotBody, err := io.ReadAll(out)
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	if string(gotBody) != "packagesdata" {
		t.Errorf("retrieved body = %q, want %q", gotBody, "packagesdata")
	}
	if !strings.EqualFold(out.Header().Get("Content-Type"), "application/octet-stream") {
		t.Errorf("retrieved Content-Type = %q", out.Header().Get("Content-Type"))
	}
}
