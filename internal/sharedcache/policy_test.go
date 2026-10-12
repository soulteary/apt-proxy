package sharedcache

import (
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestLogicalIdentityRequiresExplicitByHashAndPreservesBoundaries(t *testing.T) {
	bytes := sha256.Sum256([]byte("index"))
	digest := hex.EncodeToString(bytes[:])
	p, err := New(&Config{Repositories: []Repository{{ID: "ubuntu", Bases: []string{"https://one.example/ubuntu/", "https://two.example/archive/"}}, {ID: "security", Bases: []string{"https://security.example/ubuntu/"}}}, CoordinationDir: "/unused"})
	if err != nil {
		t.Fatal(err)
	}
	path := "dists/test/main/binary-arm64/by-hash/SHA256/" + digest
	a := httptest.NewRequest("GET", "https://one.example/ubuntu/"+path+"?channel=a", nil)
	b := httptest.NewRequest("GET", "https://two.example/archive/"+path+"?channel=a", nil)
	key := func(r *http.Request) string {
		k, e := p.RequestKey(r)
		if e != nil {
			t.Fatal(e)
		}
		return k.String()
	}
	if key(a) != key(b) {
		t.Fatal("equivalent explicitly mapped content did not share identity")
	}
	for _, other := range []string{"https://security.example/ubuntu/" + path + "?channel=a", "https://two.example/archive/" + path + "?channel=b", "https://unmapped.example/ubuntu/" + path + "?channel=a", "https://two.example/archive/dists/test/InRelease"} {
		if key(a) == key(httptest.NewRequest("GET", other, nil)) {
			t.Fatal("repository/query/mutable identity aliased")
		}
	}
	b.Header.Set("Authorization", "Bearer private")
	if key(a) == key(b) {
		t.Fatal("credentialed request shared content")
	}
	b.Header.Del("Authorization")
	b.Method = "HEAD"
	if key(a) == key(b) {
		t.Fatal("method was lost")
	}
	ka, _ := p.RequestKey(a)
	a.Header.Set("Accept-Language", "en")
	kb, _ := p.RequestKey(b)
	b.Header.Set("Accept-Language", "fr")
	if ka.Vary("Accept-Language", a).String() == kb.ForMethod("GET").Vary("Accept-Language", b).String() {
		t.Fatal("Vary was lost")
	}
	p2, err := New(&Config{Repositories: []Repository{{ID: "ubuntu", Bases: []string{"https://one.example/ubuntu/", "https://two.example/archive/"}}}, CoordinationDir: "/unused"})
	if err != nil {
		t.Fatal(err)
	}
	x := httptest.NewRequest("GET", "https://one.example/ubuntu/dists/test/InRelease", nil)
	one, _ := p.RequestKey(x)
	two, _ := p2.RequestKey(x)
	if one.String() == two.String() {
		t.Fatal("mutable objects shared across instances")
	}
}

func TestLogicalIdentityRejectsAmbiguousMappings(t *testing.T) {
	for _, cfg := range []*Config{{}, {Repositories: []Repository{{ID: "a", Bases: []string{"https://user:secret@example/ubuntu/"}}}}, {Repositories: []Repository{{ID: "a", Bases: []string{"https://example/ubuntu/"}}, {ID: "b", Bases: []string{"https://example/ubuntu/sub/"}}}}, {Repositories: []Repository{{ID: "a", Bases: []string{"https://example/ubuntu/?token=x"}}}}} {
		if _, err := New(cfg); err == nil {
			t.Fatal("invalid identity accepted")
		}
	}
}

func TestByHashVerifierRejectsWrongOrIncompleteContentBeforeDelivery(t *testing.T) {
	good := []byte("complete-index")
	hash := sha256.Sum256(good)
	digest := hex.EncodeToString(hash[:])
	body := string(good)
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Accept-Encoding") != "identity" {
			t.Error("wire representation was not pinned")
		}
		_, _ = io.WriteString(w, body)
	}))
	defer origin.Close()
	p, err := New(&Config{Repositories: []Repository{{ID: "a", Bases: []string{origin.URL + "/ubuntu/"}}}})
	if err != nil {
		t.Fatal(err)
	}
	client := &http.Client{Transport: p.WrapTransport(http.DefaultTransport, t.TempDir())}
	u := origin.URL + "/ubuntu/dists/test/main/binary-arm64/by-hash/SHA256/" + digest
	res, err := client.Get(u)
	if err != nil {
		t.Fatal(err)
	}
	got, err := io.ReadAll(res.Body)
	_ = res.Body.Close()
	if err != nil || string(got) != string(good) {
		t.Fatal("verified content changed")
	}
	body = strings.Repeat("x", len(good))
	if res, err = client.Get(u); err == nil {
		_ = res.Body.Close()
		t.Fatal("same-length bad content was exposed")
	}
}
