package httpcache_test

import (
	"reflect"
	"testing"

	. "github.com/apham0001/apt-proxy/pkg/httpcache"
)

func TestParsingCacheControl(t *testing.T) {
	table := []struct {
		ccString string
		ccStruct CacheControl
	}{
		{`public, private="set-cookie", max-age=100`, CacheControl{
			"public":  []string{},
			"private": []string{"set-cookie"},
			"max-age": []string{"100"},
		}},
		{` foo="max-age=8, space",  public`, CacheControl{
			"public": []string{},
			"foo":    []string{"max-age=8, space"},
		}},
		{`s-maxage=86400`, CacheControl{
			"s-maxage": []string{"86400"},
		}},
		{`max-stale`, CacheControl{
			"max-stale": []string{},
		}},
		{`max-stale=60`, CacheControl{
			"max-stale": []string{"60"},
		}},
		{`" max-age=8,max-age=8 "=blah`, CacheControl{
			" max-age=8,max-age=8 ": []string{"blah"},
		}},
	}

	for _, expect := range table {
		cc, err := ParseCacheControl(expect.ccString)
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(cc, expect.ccStruct) {
			t.Fatalf("cc should be equal")
		}
		if cc.String() == "" {
			t.Fatalf("cc string should not be empty")
		}
	}
}

func BenchmarkCacheControlParsing(b *testing.B) {
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := ParseCacheControl(`public, private="set-cookie", max-age=100`)
		if err != nil {
			b.Fatal(err)
		}
	}
}
