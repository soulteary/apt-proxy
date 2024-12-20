package httpcache

import (
	"bytes"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"
)

const (
	CacheControlHeader = "Cache-Control"
)

type CacheControl map[string][]string

func ParseCacheControlHeaders(h http.Header) (CacheControl, error) {
	return ParseCacheControl(strings.Join(h["Cache-Control"], ", "))
}

func ParseCacheControl(input string) (CacheControl, error) {
	cc := make(CacheControl)
	length := len(input)
	isValue := false
	lastKey := ""

	for pos := 0; pos < length; pos++ {
		var token string
		switch input[pos] {
		case '"':
			if offset := strings.IndexAny(input[pos+1:], `"`); offset != -1 {
				token = input[pos+1 : pos+1+offset]
			} else {
				token = input[pos+1:]
			}
			pos += len(token) + 1
		case ',', '\n', '\r', ' ', '\t':
			continue
		case '=':
			isValue = true
			continue
		default:
			if offset := strings.IndexAny(input[pos:], "\"\n\t\r ,="); offset != -1 {
				token = input[pos : pos+offset]
			} else {
				token = input[pos:]
			}
			pos += len(token) - 1
		}
		if isValue {
			cc.Add(lastKey, token)
			isValue = false
		} else {
			cc.Add(token, "")
			lastKey = token
		}
	}

	return cc, nil
}

func (cc CacheControl) Get(key string) (string, bool) {
	v, exists := cc[key]
	if exists && len(v) > 0 {
		return v[0], true
	}
	return "", exists
}

func (cc CacheControl) Add(key, val string) {
	if !cc.Has(key) {
		cc[key] = []string{}
	}
	if val != "" {
		cc[key] = append(cc[key], val)
	}
}

func (cc CacheControl) Has(key string) bool {
	_, exists := cc[key]
	return exists
}

func (cc CacheControl) Duration(key string) (time.Duration, error) {
	d, _ := cc.Get(key)
	return time.ParseDuration(d + "s")
}

func (cc CacheControl) String() string {
	keys := make([]string, len(cc))
	for k := range cc {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	buf := bytes.Buffer{}

	for _, k := range keys {
		vals := cc[k]
		if len(vals) == 0 {
			buf.WriteString(k + ", ")
		}
		for _, val := range vals {
			buf.WriteString(fmt.Sprintf("%s=%q, ", k, val))
		}
	}

	return strings.TrimSuffix(buf.String(), ", ")
}
