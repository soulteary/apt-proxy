package metalink

import (
	"context"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
)

type statusBody struct {
	io.Reader
	closes atomic.Int32
}

func (b *statusBody) Close() error { b.closes.Add(1); return nil }

type statusReply struct {
	status int
	header http.Header
	body   string
}

// Every candidate passes independent repomd verification; artifact failures
// therefore exercise mirror fallback rather than discovery failure.
func statusTransport(t *testing.T, replies [2]statusReply) (http.RoundTripper, *http.Request, *[]*statusBody, *[]string) {
	t.Helper()
	c, err := New(&Config{PublicURL: "http://proxy.example", AllowedMirrorHosts: []string{"first.example", "second.example"}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	const repomd = "verified repomd"
	m, err := c.parse([]byte(document(repomd, resourceURL("http://first.example/repo", 100)+resourceURL("http://second.example/repo", 99))))
	if err != nil {
		t.Fatal(err)
	}
	s := &snapshot{repositoryToken: strings.Repeat("a", 64), metadata: m}
	route := &route{snapshot: s, relative: "Packages/pkg.rpm"}
	u := &url.URL{Scheme: "http", Host: canonicalHost, Path: "/" + c.policy + "/" + s.repositoryToken + "/" + m.digest + "/" + route.relative}
	r, err := http.NewRequestWithContext(context.WithValue(context.Background(), routeKey{}, route), http.MethodGet, u.String(), nil)
	if err != nil {
		t.Fatal(err)
	}
	bodies := make([]*statusBody, 0, 2)
	hosts := make([]string, 0, 2)
	c.transport = roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if strings.HasSuffix(req.URL.Path, "/repodata/repomd.xml") {
			return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(repomd)), ContentLength: int64(len(repomd)), Request: req}, nil
		}
		index := 0
		if req.URL.Hostname() == "second.example" {
			index = 1
		}
		if req.URL.Path != "/repo/Packages/pkg.rpm" {
			t.Errorf("unexpected artifact path: %s", req.URL.Path)
		}
		reply := replies[index]
		body := &statusBody{Reader: strings.NewReader(reply.body)}
		bodies = append(bodies, body)
		hosts = append(hosts, req.URL.Hostname())
		h := reply.header.Clone()
		if h == nil {
			h = make(http.Header)
		}
		return &http.Response{StatusCode: reply.status, Header: h, Body: body, ContentLength: int64(len(reply.body)), Request: req}, nil
	})
	return c.WrapTransport(roundTripFunc(func(*http.Request) (*http.Response, error) {
		t.Fatal("canonical request escaped Metalink routing")
		return nil, nil
	})), r, &bodies, &hosts
}

func TestArtifactFailureStatusesUseVerifiedFallback(t *testing.T) {
	for _, status := range []int{http.StatusUnauthorized, http.StatusForbidden, http.StatusNotFound, http.StatusRequestTimeout, http.StatusConflict, http.StatusRequestedRangeNotSatisfiable, http.StatusTooManyRequests, http.StatusInternalServerError, http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusMovedPermanently, http.StatusFound, http.StatusTemporaryRedirect, http.StatusPermanentRedirect, http.StatusNoContent, http.StatusResetContent, http.StatusSwitchingProtocols} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			transport, r, bodies, hosts := statusTransport(t, [2]statusReply{{status: status, body: "failed mirror"}, {status: http.StatusOK, body: "package payload"}})
			canonical := r.URL.String()
			res, err := transport.RoundTrip(r)
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = res.Body.Close() }()
			data, err := io.ReadAll(res.Body)
			if err != nil || res.StatusCode != http.StatusOK || string(data) != "package payload" {
				t.Fatalf("verified fallback failed: status=%d body=%q err=%v", res.StatusCode, data, err)
			}
			if len(*hosts) != 2 || (*hosts)[0] != "first.example" || (*hosts)[1] != "second.example" {
				t.Fatalf("remaining candidate was not used: %v", *hosts)
			}
			if (*bodies)[0].closes.Load() != 1 {
				t.Fatal("failed mirror body was not closed before fallback")
			}
			if r.URL.String() != canonical || res.Request.URL.String() != canonical {
				t.Fatal("mirror failure changed canonical cache identity")
			}
		})
	}
}

func TestAllArtifactCandidatesFailAndCloseBodies(t *testing.T) {
	transport, r, bodies, hosts := statusTransport(t, [2]statusReply{{status: http.StatusTooManyRequests, body: "throttled"}, {status: http.StatusForbidden, body: "denied"}})
	canonical := r.URL.String()
	res, err := transport.RoundTrip(r)
	if res != nil {
		_ = res.Body.Close()
	}
	if err == nil || res != nil {
		t.Fatal("failed candidates exposed an error representation as an artifact")
	}
	if len(*hosts) != 2 || len(*bodies) != 2 {
		t.Fatal("not every verified candidate was attempted")
	}
	for _, body := range *bodies {
		if body.closes.Load() != 1 {
			t.Fatal("failed response body leaked")
		}
	}
	if r.URL.String() != canonical {
		t.Fatal("all-failed request changed canonical cache identity")
	}
}

func TestArtifactPartialAndConditionalStatusAcceptance(t *testing.T) {
	multipartBody := "--safe-boundary\r\nContent-Range: bytes 0-1/10\r\n\r\n01\r\n--safe-boundary\r\nContent-Range: bytes 4-5/10\r\n\r\n45\r\n--safe-boundary--\r\n"
	for _, tc := range []struct {
		name              string
		status            int
		method            string
		request, response http.Header
		body              string
		accepted          bool
	}{
		{name: "unsolicited partial", status: http.StatusPartialContent, response: http.Header{"Content-Range": []string{"bytes 0-3/10"}}, body: "0123"},
		{name: "requested single range", status: http.StatusPartialContent, request: http.Header{"Range": []string{"bytes=0-3"}}, response: http.Header{"Content-Range": []string{"bytes 0-3/10"}}, body: "0123", accepted: true},
		{name: "partial lacks range metadata", status: http.StatusPartialContent, request: http.Header{"Range": []string{"bytes=0-3"}}, body: "0123"},
		{name: "malformed content range", status: http.StatusPartialContent, request: http.Header{"Range": []string{"bytes=0-3"}}, response: http.Header{"Content-Range": []string{"not a byte range"}}, body: "0123"},
		{name: "duplicate content range", status: http.StatusPartialContent, request: http.Header{"Range": []string{"bytes=0-3"}}, response: http.Header{"Content-Range": []string{"bytes 0-3/10", "bytes 0-3/10"}}, body: "0123"},
		{name: "requested multipart range", status: http.StatusPartialContent, request: http.Header{"Range": []string{"bytes=0-1,4-5"}}, response: http.Header{"Content-Type": []string{"multipart/byteranges; boundary=safe-boundary"}}, body: multipartBody, accepted: true},
		{name: "multipart lacks boundary", status: http.StatusPartialContent, request: http.Header{"Range": []string{"bytes=0-1,4-5"}}, response: http.Header{"Content-Type": []string{"multipart/byteranges"}}, body: multipartBody},
		{name: "invalid multipart boundary", status: http.StatusPartialContent, request: http.Header{"Range": []string{"bytes=0-1,4-5"}}, response: http.Header{"Content-Type": []string{`multipart/byteranges; boundary="invalid@boundary"`}}, body: multipartBody},
		{name: "head partial", status: http.StatusPartialContent, method: http.MethodHead, request: http.Header{"Range": []string{"bytes=0-3"}}, response: http.Header{"Content-Range": []string{"bytes 0-3/10"}}, body: "0123"},
		{name: "unsolicited not modified", status: http.StatusNotModified},
		{name: "etag revalidation", status: http.StatusNotModified, request: http.Header{"If-None-Match": []string{`"v1"`}}, accepted: true},
		{name: "weak etag list", status: http.StatusNotModified, request: http.Header{"If-None-Match": []string{`W/"v1", "v2"`}}, accepted: true},
		{name: "etag wildcard", status: http.StatusNotModified, request: http.Header{"If-None-Match": []string{"*"}}, accepted: true},
		{name: "invalid etag", status: http.StatusNotModified, request: http.Header{"If-None-Match": []string{"unquoted"}}},
		{name: "date revalidation", status: http.StatusNotModified, request: http.Header{"If-Modified-Since": []string{"Mon, 02 Jan 2006 15:04:05 GMT"}}, accepted: true},
		{name: "invalid date", status: http.StatusNotModified, request: http.Header{"If-Modified-Since": []string{"invalid"}}},
		{name: "head revalidation", status: http.StatusNotModified, method: http.MethodHead, request: http.Header{"If-None-Match": []string{`"v1"`}}, accepted: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			transport, r, bodies, hosts := statusTransport(t, [2]statusReply{{status: tc.status, header: tc.response, body: tc.body}, {status: http.StatusOK, body: "fallback"}})
			if tc.method != "" {
				r.Method = tc.method
			}
			r.Header = tc.request.Clone()
			if r.Header == nil {
				r.Header = make(http.Header)
			}
			res, err := transport.RoundTrip(r)
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = res.Body.Close() }()
			if tc.accepted {
				if res.StatusCode != tc.status || len(*hosts) != 1 {
					t.Fatal("valid range or revalidation response rejected")
				}
			} else {
				if res.StatusCode != http.StatusOK || len(*hosts) != 2 || (*bodies)[0].closes.Load() != 1 {
					t.Fatal("unsolicited or malformed response prevented verified fallback")
				}
			}
		})
	}
}
