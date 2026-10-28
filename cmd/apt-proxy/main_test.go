package main

import (
	"io"
	"os"
	"strings"
	"testing"

	"github.com/soulteary/apt-proxy/internal/cli"
)

func commandArgs(t *testing.T, args ...string) {
	t.Helper()
	t.Chdir(t.TempDir())
	for _, entry := range os.Environ() {
		name, _, _ := strings.Cut(entry, "=")
		if strings.HasPrefix(name, "APT_PROXY_") {
			t.Setenv(name, "")
		}
	}
	original := os.Args
	os.Args = append([]string{"apt-proxy"}, args...)
	t.Cleanup(func() { os.Args = original })
}

func TestMainRepositoryHelpAndBuildMetadata(t *testing.T) {
	commandArgs(t, "repository", "--help")
	old := os.Stdout
	output, err := os.CreateTemp(t.TempDir(), "output")
	if err != nil {
		t.Fatal(err)
	}
	defer output.Close()
	os.Stdout = output
	defer func() { os.Stdout = old }()
	previousVersion, previousCommit, previousDate := cli.BuildInfo()
	defer cli.SetBuildInfo(previousVersion, previousCommit, previousDate)
	oldVersion, oldCommit, oldDate := version, commit, date
	version, commit, date = "v-test", "test-commit", "test-date"
	defer func() { version, commit, date = oldVersion, oldCommit, oldDate }()
	main()
	v, c, d := cli.BuildInfo()
	if v != version || c != commit || d != date {
		t.Fatalf("metadata=%s %s %s", v, c, d)
	}
	if _, err = output.Seek(0, 0); err != nil {
		t.Fatal(err)
	}
	body, err := io.ReadAll(output)
	if err != nil || !strings.Contains(string(body), "Usage: apt-proxy repository") {
		t.Fatalf("help=%q err=%v", body, err)
	}
}

func TestRunPropagatesCommandErrors(t *testing.T) {
	for _, tt := range []struct {
		name string
		args []string
		want string
	}{
		{"repository", []string{"repository"}, "repository command required"},
		{"flags", []string{"--does-not-exist"}, "parsing flags"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			commandArgs(t, tt.args...)
			err := run()
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("error=%v, want %q", err, tt.want)
			}
		})
	}
}

func TestRunPropagatesDaemonValidation(t *testing.T) {
	commandArgs(t)
	path := t.TempDir() + "/config.yaml"
	if err := os.WriteFile(path, []byte("security:\n  download_allow_cidrs: [invalid-cidr]\n"), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("APT_PROXY_CONFIG_FILE", path)
	err := run()
	if err == nil || !strings.Contains(err.Error(), "invalid CIDR") {
		t.Fatalf("daemon error=%v", err)
	}
}
