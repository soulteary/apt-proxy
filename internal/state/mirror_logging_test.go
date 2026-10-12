package state

import (
	"bytes"
	"strings"
	"testing"

	"github.com/soulteary/apt-proxy/internal/distro"
	logger "github.com/soulteary/logger-kit/v3"
)

func TestInvalidMirrorLogsRedactCredentials(t *testing.T) {
	var output bytes.Buffer
	previous := logger.Default()
	logger.SetDefault(logger.New(logger.Config{Level: logger.WarnLevel, Output: &output, Format: logger.FormatJSON}))
	t.Cleanup(func() { logger.SetDefault(previous) })
	for _, raw := range []string{
		"https://access-token-secret%zz@mirror.example/ubuntu/",
		"https://username-secret:password-secret%zz@mirror.example/ubuntu/",
	} {
		output.Reset()
		mirror := NewMirrorState(distro.TypeUbuntu)
		mirror.Set("https://mirror.example/ubuntu/")
		mirror.Set(raw)
		if mirror.Get() != nil {
			t.Fatal("invalid mirror failed to clear the previous state")
		}
		logs := output.String()
		for _, secret := range []string{"access-token-secret", "username-secret", "password-secret"} {
			if strings.Contains(logs, secret) {
				t.Fatalf("invalid mirror diagnostic leaked %q: %s", secret, logs)
			}
		}
		if !strings.Contains(logs, "invalid mirror URL, clearing state") || !strings.Contains(logs, "invalid URL escape") {
			t.Fatalf("invalid mirror diagnosis was lost: %s", logs)
		}
	}
}
