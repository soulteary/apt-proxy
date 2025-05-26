package httpcache_test

import (
	"net/url"
	"testing"

	"github.com/apham0001/apt-proxy/pkg/httpcache"
)

func mustParseUrl(u string) *url.URL {
	ru, err := url.Parse(u)
	if err != nil {
		panic(err)
	}
	return ru
}

func TestKeysDiffer(t *testing.T) {
	k1 := httpcache.NewKey("GET", mustParseUrl("http://x.org/test"), nil)
	k2 := httpcache.NewKey("GET", mustParseUrl("http://y.org/test"), nil)

	if k1.String() == k2.String() {
		t.Fatal("key should be same")
	}
}

func TestRequestKey(t *testing.T) {
	r := newRequest("GET", "http://x.org/test")

	k1 := httpcache.NewKey("GET", mustParseUrl("http://x.org/test"), nil)
	k2 := httpcache.NewRequestKey(r)

	if k1.String() != k2.String() {
		t.Fatal("request key should be same")
	}
}

func TestVaryKey(t *testing.T) {
	r := newRequest("GET", "http://x.org/test", "Llamas-1: true", "Llamas-2: false")

	k1 := httpcache.NewRequestKey(r)
	k2 := httpcache.NewRequestKey(r).Vary("Llamas-1, Llamas-2", r)

	if k1.String() == k2.String() {
		t.Fatal("vary key should be same")
	}
}

func TestRequestKeyWithContentLocation(t *testing.T) {
	r := newRequest("GET", "http://x.org/test1", "Content-Location: http://x.org/test2")

	k1 := httpcache.NewKey("GET", mustParseUrl("http://x.org/test2"), nil)
	k2 := httpcache.NewRequestKey(r)

	if k1.String() != k2.String() {
		t.Fatal("request key should with content location")
	}
}

func TestRequestKeyWithIllegalContentLocation(t *testing.T) {
	r := newRequest("GET", "http://x.org/test1", "Content-Location: http://y.org/test2")

	k1 := httpcache.NewKey("GET", mustParseUrl("http://x.org/test1"), nil)
	k2 := httpcache.NewRequestKey(r)

	if k1.String() != k2.String() {
		t.Fatal("request key should with illegal content location")
	}
}
