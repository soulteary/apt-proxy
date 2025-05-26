package httpcache_test

import (
	"net/http"
	"reflect"
	"strings"
	"testing"

	"github.com/apham0001/apt-proxy/pkg/httpcache"
)

func TestSaveResource(t *testing.T) {
	var body = strings.Repeat("llamas", 5000)
	var cache = httpcache.NewMemoryCache()

	res := httpcache.NewResourceBytes(http.StatusOK, []byte(body), http.Header{
		"Llamas": []string{"true"},
	})

	if err := cache.Store(res, "testkey"); err != nil {
		t.Fatal(err)
	}

	resOut, err := cache.Retrieve("testkey")
	if err != nil {
		t.Fatal(err)
	}

	if resOut == nil {
		t.Fatalf("resOut should not be null")
	}

	if !reflect.DeepEqual(res.Header(), resOut.Header()) {
		t.Fatalf("header should be equal")
	}

	if body != readAllString(resOut) {
		t.Fatalf("body should be equal")
	}
}

func TestSaveResourceWithIncorrectContentLength(t *testing.T) {
	var body = "llamas"
	var cache = httpcache.NewMemoryCache()

	res := httpcache.NewResourceBytes(http.StatusOK, []byte(body), http.Header{
		"Llamas":         []string{"true"},
		"Content-Length": []string{"10"},
	})

	if err := cache.Store(res, "testkey"); err == nil {
		t.Fatal("Entry should have generated an error")
	}

	_, err := cache.Retrieve("testkey")
	if err != httpcache.ErrNotFoundInCache {
		t.Fatal("Entry shouldn't have been cached")
	}
}
