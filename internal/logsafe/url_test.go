package logsafe

import (
	"errors"
	"fmt"
	"net/url"
	"strings"
	"testing"
)

func TestURLRedactsSecretsWithoutMutatingRouting(t *testing.T) {
	raw := "https://alice:password-secret@mirror.example/ubuntu/?token=query-secret&custom_auth=custom-secret&token=another-secret#fragment-secret"
	got := URL(raw)
	for _, secret := range []string{"password-secret", "query-secret", "custom-secret", "another-secret", "fragment-secret"} {
		if strings.Contains(got, secret) {
			t.Fatalf("secret %q retained in %q", secret, got)
		}
	}
	u, err := url.Parse(got)
	if err != nil {
		t.Fatal(err)
	}
	if u.Host != "mirror.example" || u.Path != "/ubuntu/" || u.Query().Get("token") != "[redacted]" || u.Query().Get("custom_auth") != "[redacted]" {
		t.Fatalf("lost diagnostic origin, path or query keys: %q", got)
	}
	if URL("https://mirror.example/ubuntu/") != "https://mirror.example/ubuntu/" {
		t.Fatal("ordinary mirror URL changed")
	}
	if !strings.Contains(raw, "password-secret") || !strings.Contains(raw, "custom-secret") {
		t.Fatal("routing input was modified")
	}
}

func TestURLRedactsMalformedInputs(t *testing.T) {
	for _, raw := range []string{
		"https://alice:password-secret%zz@mirror.example/?token=query-secret",
		"https://mirror.example/?token=query-secret&bad=%zz",
		"https:alice:password-secret@mirror.example/?token=query-secret",
	} {
		got := URL(raw)
		if strings.Contains(got, "password-secret") || strings.Contains(got, "query-secret") {
			t.Fatalf("malformed URL exposed secret: %q", got)
		}
	}
}

func TestErrorRedactsWrappedAndJoinedRequestURLs(t *testing.T) {
	requestErr := &url.Error{Op: "Get", URL: "https://alice:password-secret@mirror.example/ubuntu/?token=query-secret", Err: errors.New("connection refused")}
	err := fmt.Errorf("mirror election failed: %w", errors.Join(requestErr, errors.New("no healthy mirror")))
	got := Error(err).Error()
	if strings.Contains(got, "password-secret") || strings.Contains(got, "query-secret") {
		t.Fatalf("request error leaked secret: %s", got)
	}
	for _, detail := range []string{"mirror election failed", "mirror.example/ubuntu/", "connection refused", "no healthy mirror"} {
		if !strings.Contains(got, detail) {
			t.Fatalf("diagnostic %q missing from %s", detail, got)
		}
	}
	if !strings.Contains(err.Error(), "password-secret") || !errors.Is(err, requestErr) {
		t.Fatal("original caller error was changed")
	}
	if Error(nil) != nil {
		t.Fatal("nil error changed")
	}
}
