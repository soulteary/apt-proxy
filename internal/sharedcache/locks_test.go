//go:build unix

package sharedcache

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func TestFileCoordinatorCancellationAndCrashRelease(t *testing.T) {
	if os.Getenv("APT_PROXY_LOCK_CHILD") == "1" {
		c, err := NewFileCoordinator(os.Getenv("APT_PROXY_LOCK_DIR"))
		if err != nil {
			panic(err)
		}
		lease, err := c.Acquire(context.Background(), "key")
		if err != nil {
			panic(err)
		}
		defer func() { _ = lease.Close() }()
		fmt.Println("owned")
		select {}
	}
	dir := t.TempDir()
	c, err := NewFileCoordinator(dir)
	if err != nil {
		t.Fatal(err)
	}
	leader, err := c.Acquire(context.Background(), "key")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if lease, err := c.Acquire(ctx, "key"); lease != nil || !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("follower cancellation failed: %v", err)
	}
	_ = leader.Close()
	_ = leader.Close()
	child := exec.Command(os.Args[0], "-test.run=^TestFileCoordinatorCancellationAndCrashRelease$")
	child.Env = append(os.Environ(), "APT_PROXY_LOCK_CHILD=1", "APT_PROXY_LOCK_DIR="+dir)
	stdout, err := child.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err = child.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = child.Process.Kill(); _ = child.Wait() })
	ready := make(chan string, 1)
	go func() {
		scanner := bufio.NewScanner(stdout)
		if scanner.Scan() {
			ready <- scanner.Text()
		}
	}()
	select {
	case got := <-ready:
		if got != "owned" {
			t.Fatal(got)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("child never owned lock")
	}
	_ = child.Process.Kill()
	_ = child.Wait()
	ctx2, cancel2 := context.WithTimeout(context.Background(), time.Second)
	defer cancel2()
	lease, err := c.Acquire(ctx2, "key")
	if err != nil {
		t.Fatal("crashed owner retained lock", err)
	}
	_ = lease.Close()
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) != 1 {
		t.Fatal("lock file lifecycle changed")
	}
	link := filepath.Join(t.TempDir(), "link")
	if err = os.Symlink(dir, link); err != nil {
		t.Fatal(err)
	}
	if _, err = NewFileCoordinator(link); err == nil {
		t.Fatal("symlink coordinator accepted")
	}
}
