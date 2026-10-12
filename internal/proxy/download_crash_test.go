package proxy

import (
	"bufio"
	"fmt"
	"io"
	"net/http/httptest"
	"os"
	"os/exec"
	"runtime"
	"testing"
	"time"
)

func TestSpoolDoesNotSurviveProcessKill(t *testing.T) {
	if runtime.GOOS != "linux" && runtime.GOOS != "darwin" {
		t.Skip("the released platforms support unlinking open files")
	}
	if os.Getenv("APT_PROXY_TEST_SPOOL_CHILD") == "1" {
		transport := budgetTestTransport(os.Getenv("APT_PROXY_TEST_SPOOL_DIR"), 8, 8)
		res, err := transport.RoundTrip(httptest.NewRequest("GET", "http://mirror.example/package.deb", nil))
		if err != nil {
			t.Fatal(err)
		}
		// Keep the descriptor alive until the parent kills this process. No body
		// Close, deferred cleanup or graceful shutdown can run in that case.
		defer res.Body.Close()
		fmt.Println("spool-ready")
		time.Sleep(time.Hour)
		return
	}
	dir := t.TempDir()
	cmd := exec.Command(os.Args[0], "-test.run=^TestSpoolDoesNotSurviveProcessKill$")
	cmd.Env = append(os.Environ(), "APT_PROXY_TEST_SPOOL_CHILD=1", "APT_PROXY_TEST_SPOOL_DIR="+dir)
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
			if scanner.Text() == "spool-ready" {
				ready <- true
				return
			}
		}
		ready <- false
	}()
	select {
	case ok := <-ready:
		if !ok {
			t.Fatal("child did not create its spool")
		}
	case <-time.After(10 * time.Second):
		t.Fatal("timed out waiting for child spool")
	}
	assertNoSpoolNames(t, dir)
	if err := cmd.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	err = cmd.Wait()
	waited = true
	if err == nil {
		t.Fatal("child unexpectedly exited normally")
	}
	assertNoSpoolNames(t, dir)
	transport := budgetTestTransport(dir, 8, 8)
	res, err := transport.RoundTrip(httptest.NewRequest("GET", "http://mirror.example/package.deb", nil))
	if err != nil {
		t.Fatal(err)
	}
	body, err := io.ReadAll(res.Body)
	if closeErr := res.Body.Close(); closeErr != nil {
		t.Fatal(closeErr)
	}
	if err != nil || string(body) != "12345678" || transport.budget.used != 0 {
		t.Fatalf("restart body=%q err=%v budget=%d", body, err, transport.budget.used)
	}
	assertNoSpoolNames(t, dir)
}

func assertNoSpoolNames(t *testing.T, dir string) {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) != 0 {
		t.Fatalf("spool directory retained entries: %v %v", entries, err)
	}
}

func TestUnlinkedSpoolKeepsBudgetAndDoesNotRemoveReplacement(t *testing.T) {
	dir := t.TempDir()
	transport := budgetTestTransport(dir, 8, 8)
	req := httptest.NewRequest("GET", "http://mirror.example/package.deb", nil)
	res, err := transport.RoundTrip(req)
	if err != nil {
		t.Fatal(err)
	}
	spool := res.Body.(*downloadFile)
	defer res.Body.Close()
	assertNoSpoolNames(t, dir)
	if transport.budget.used != 8 {
		t.Fatal("unlink prematurely released disk budget")
	}
	if other, err := transport.RoundTrip(req); err == nil {
		other.Body.Close()
		t.Fatal("open anonymous spool did not consume budget")
	}
	if err := os.WriteFile(spool.path, []byte("replacement"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := spool.Close(); err != nil {
		t.Fatal(err)
	}
	if err := spool.Close(); err != nil {
		t.Fatal(err)
	}
	content, err := os.ReadFile(spool.path)
	if err != nil || string(content) != "replacement" || transport.budget.used != 0 {
		t.Fatalf("close removed unrelated file or miscounted budget: %q %v budget=%d", content, err, transport.budget.used)
	}
}
