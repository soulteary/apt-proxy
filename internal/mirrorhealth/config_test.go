package mirrorhealth

import "testing"

func TestConfigRejectsAmbiguousAndCredentialMappings(t *testing.T) {
	for _, mirrors := range [][]string{
		{"https://user:secret@primary.example/ubuntu/", "https://secondary.example/archive/"},
		{"https://primary.example/ubuntu/?token=secret", "https://secondary.example/archive/"},
		{"https://primary.example/ubuntu/", "https://primary.example/ubuntu/"},
		{"https://primary.example/ubuntu/", "https://primary.example/ubuntu/nested/"},
		{"https://primary.example/../ubuntu/", "https://secondary.example/archive/"},
		{"https://primary.example/ubuntu", "https://secondary.example/archive/"},
	} {
		cfg := &Config{Enabled: true, Repositories: []Repository{{ID: "ubuntu", Mirrors: mirrors}}}
		if err := cfg.Validate(); err == nil {
			t.Fatalf("unsafe mapping accepted: %v", mirrors)
		}
	}
	cfg := &Config{Enabled: true, Repositories: []Repository{{ID: "ubuntu", Mirrors: []string{"https://primary.example/ubuntu/", "https://secondary.example/archive/"}, PublicQueryKeys: []string{"access_token"}}}}
	if err := cfg.Validate(); err == nil {
		t.Fatal("credential query allowed across origins")
	}
	for _, key := range []string{"session", "username", "pwd", "credentials"} {
		cfg.Repositories[0].PublicQueryKeys = []string{key}
		if err := cfg.Validate(); err == nil {
			t.Fatalf("credential-like query %s permitted", key)
		}
	}
	if err := (&Config{Enabled: true, ProbeTimeoutSeconds: -1}).Validate(); err == nil {
		t.Fatal("invalid limits accepted")
	}
}
