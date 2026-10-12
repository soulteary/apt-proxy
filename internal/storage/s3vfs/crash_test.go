package s3vfs

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	httpcache "github.com/soulteary/httpcache-kit/v4"
)

type blockedSpillUpload struct{ *memoryS3 }

func (b *blockedSpillUpload) RoundTrip(r *http.Request) (*http.Response, error) {
	if r.Method == http.MethodPut {
		// The cache has finished writing its body to the VFS and Close is now
		// uploading it. No application cleanup can run after the parent's kill.
		fmt.Println("s3-spill-ready")
		<-r.Context().Done()
		return nil, r.Context().Err()
	}
	return b.memoryS3.RoundTrip(r)
}

func crashTestVFS(t *testing.T, dir string, blockUpload bool) *S3VFS {
	t.Helper()
	backend := &memoryS3{objects: map[string]string{}}
	var transport http.RoundTripper = backend
	if blockUpload {
		transport = &blockedSpillUpload{backend}
	}
	fs, err := New(context.Background(), Config{
		Endpoint: "s3.test", Region: "us-east-1", Bucket: "test-bucket", Prefix: "cache",
		UsePathStyle: true, InlineMaxBytes: 16, TempDir: dir,
		HTTPClient: &http.Client{Transport: transport},
	})
	if err != nil {
		t.Fatal(err)
	}
	return fs
}

func assertNoNamedS3Spills(t *testing.T, dir string) {
	t.Helper()
	names, err := filepath.Glob(filepath.Join(dir, "s3vfs-*.bin"))
	if err != nil || len(names) != 0 {
		t.Fatalf("named S3 spills remain: names=%v err=%v", names, err)
	}
}

func TestS3SpillDoesNotSurviveRepeatedProcessKill(t *testing.T) {
	if runtime.GOOS != "linux" && runtime.GOOS != "darwin" {
		t.Skip("released platforms support unlinking open files")
	}
	if os.Getenv("APT_PROXY_TEST_S3_SPILL_CHILD") == "1" {
		fs := crashTestVFS(t, os.Getenv("APT_PROXY_TEST_S3_SPILL_DIR"), true)
		cache := httpcache.NewVFSCacheWithConfig(fs, nil)
		defer cache.Close()
		res := httpcache.NewResourceBytes(200, bytes.Repeat([]byte("x"), 65536), http.Header{"Content-Length": {"65536"}})
		defer res.Close()
		if err := cache.Store(res, "GET:http://mirror.example/ubuntu/pool/large.deb"); err != nil {
			t.Fatal(err)
		}
		t.Fatal("blocked S3 upload unexpectedly returned")
	}
	dir := t.TempDir()
	neighbor := filepath.Join(dir, "another-instance-active.tmp")
	if err := os.WriteFile(neighbor, []byte("keep"), 0600); err != nil {
		t.Fatal(err)
	}
	for cycle := 0; cycle < 2; cycle++ {
		cmd := exec.Command(os.Args[0], "-test.run=^TestS3SpillDoesNotSurviveRepeatedProcessKill$")
		cmd.Env = append(os.Environ(), "APT_PROXY_TEST_S3_SPILL_CHILD=1", "APT_PROXY_TEST_S3_SPILL_DIR="+dir)
		stdout, err := cmd.StdoutPipe()
		if err != nil {
			t.Fatal(err)
		}
		cmd.Stderr = os.Stderr
		if err := cmd.Start(); err != nil {
			t.Fatal(err)
		}
		waited := false
		t.Cleanup(func() {
			if !waited {
				_ = cmd.Process.Kill()
				_ = cmd.Wait()
			}
		})
		ready := make(chan bool, 1)
		go func() {
			scanner := bufio.NewScanner(stdout)
			for scanner.Scan() {
				if scanner.Text() == "s3-spill-ready" {
					ready <- true
					return
				}
			}
			ready <- false
		}()
		select {
		case ok := <-ready:
			if !ok {
				t.Fatal("child exited before the spill upload")
			}
		case <-time.After(10 * time.Second):
			t.Fatal("timed out waiting for spill upload")
		}
		assertNoNamedS3Spills(t, dir)
		if err := cmd.Process.Kill(); err != nil {
			t.Fatal(err)
		}
		if err := cmd.Wait(); err == nil {
			t.Fatal("child unexpectedly exited normally")
		}
		waited = true
		assertNoNamedS3Spills(t, dir)

		// Restart the real VFS cache and store/retrieve another spilled object.
		// This proves anonymous staging remains uploadable after a crash and
		// that startup never deletes another instance's temporary file.
		cache := httpcache.NewVFSCacheWithConfig(crashTestVFS(t, dir, false), nil)
		payload := []byte("complete package after process restart")
		res := httpcache.NewResourceBytes(200, payload, http.Header{"Content-Length": {fmt.Sprint(len(payload))}})
		if err := cache.Store(res, "restart"); err != nil {
			t.Fatal(err)
		}
		_ = res.Close()
		stored, err := cache.Retrieve("restart")
		if err != nil {
			t.Fatal(err)
		}
		body, readErr := io.ReadAll(stored)
		_ = stored.Close()
		if readErr != nil || !bytes.Equal(body, payload) {
			t.Fatalf("restart retrieval: body=%q err=%v", body, readErr)
		}
		if err := cache.Close(); err != nil {
			t.Fatal(err)
		}
		assertNoNamedS3Spills(t, dir)
		if body, err := os.ReadFile(neighbor); err != nil || string(body) != "keep" {
			t.Fatalf("shared TempDir file modified: body=%q err=%v", body, err)
		}
	}
}

func TestS3SpillClosePreservesReplacementPath(t *testing.T) {
	fs, backend := newMemoryVFS(t, 4)
	file, err := fs.OpenFile("body/package.deb", os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0600)
	if err != nil {
		t.Fatal(err)
	}
	w := file.(*s3WFile)
	defer file.Close()
	payload := "package bytes"
	if _, err := file.Write([]byte(payload)); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(w.tmp.Name()); !os.IsNotExist(err) {
		t.Fatalf("spill still has a name: %v", err)
	}
	if err := os.WriteFile(w.tmp.Name(), []byte("replacement"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	if body, err := os.ReadFile(w.tmp.Name()); err != nil || string(body) != "replacement" {
		t.Fatalf("Close touched a reused spill path: body=%q err=%v", body, err)
	}
	if backend.objects["cache/body/package.deb"] != payload {
		t.Fatalf("anonymous spill upload=%q", backend.objects["cache/body/package.deb"])
	}
}
