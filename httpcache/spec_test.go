package httpcache_test

import (
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"testing"
	"time"

	"github.com/lox/httpcache"
	"github.com/lox/httpcache/httplog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func testSetup() (*client, *upstreamServer) {
	upstream := &upstreamServer{
		Body:    []byte("llamas"),
		asserts: []func(r *http.Request){},
		Now:     time.Date(2009, time.November, 10, 23, 0, 0, 0, time.UTC),
		Header:  http.Header{},
	}

	httpcache.Clock = func() time.Time {
		return upstream.Now
	}

	cacheHandler := httpcache.NewHandler(
		httpcache.NewMemoryCache(),
		upstream,
	)

	var handler http.Handler = cacheHandler

	if testing.Verbose() {
		rlogger := httplog.NewResponseLogger(cacheHandler)
		rlogger.DumpRequests = true
		rlogger.DumpResponses = true
		handler = rlogger
		httpcache.DebugLogging = true
	} else {
		log.SetOutput(ioutil.Discard)
	}

	return &client{handler, cacheHandler}, upstream
}

func TestSpecResponseCacheControl(t *testing.T) {
	var cases = []struct {
		cacheControl   string
		cacheStatus    string
		requests       int
		secondsElapsed time.Duration
		shared         bool
	}{
		{cacheControl: "", requests: 2},
		{cacheControl: "no-cache", requests: 2, cacheStatus: "SKIP"},
		{cacheControl: "no-store", requests: 2, cacheStatus: "SKIP"},
		{cacheControl: "max-age=0, no-cache", requests: 2, cacheStatus: "SKIP"},
		{cacheControl: "max-age=0", requests: 2, cacheStatus: "SKIP"},
		{cacheControl: "s-maxage=0", requests: 2, cacheStatus: "SKIP", shared: true},
		{cacheControl: "s-maxage=60", requests: 2, cacheStatus: "HIT", shared: true},
		{cacheControl: "s-maxage=60", requests: 2, secondsElapsed: 65, shared: true},
		{cacheControl: "max-age=60", requests: 1, cacheStatus: "HIT"},
		{cacheControl: "max-age=60", requests: 1, secondsElapsed: 35, cacheStatus: "HIT"},
		{cacheControl: "max-age=60", requests: 2, secondsElapsed: 65},
		{cacheControl: "max-age=60, must-revalidate", requests: 2, cacheStatus: "HIT"},
		{cacheControl: "max-age=60, proxy-revalidate", requests: 1, cacheStatus: "HIT"},
		{cacheControl: "max-age=60, proxy-revalidate", requests: 2, cacheStatus: "HIT", shared: true},
		{cacheControl: "private, max-age=60", requests: 1, cacheStatus: "HIT"},
		{cacheControl: "private, max-age=60", requests: 2, cacheStatus: "SKIP", shared: true},
	}

	for idx, c := range cases {
		client, upstream := testSetup()
		upstream.CacheControl = c.cacheControl
		client.cacheHandler.Shared = c.shared

		assert.Equal(t, http.StatusOK, client.get("/").Code)
		upstream.timeTravel(time.Second * time.Duration(c.secondsElapsed))

		r := client.get("/")
		assert.Equal(t, http.StatusOK, r.statusCode)
		require.Equal(t, c.requests, upstream.requests,
			fmt.Sprintf("case #%d failed, %+v", idx+1, c))

		if c.cacheStatus != "" {
			require.Equal(t, c.cacheStatus, r.cacheStatus,
				fmt.Sprintf("case #%d failed, %+v", idx+1, c))
		}
	}
}

func TestSpecResponseCacheControlWithPrivateHeaders(t *testing.T) {
	client, upstream := testSetup()
	client.cacheHandler.Shared = false
	upstream.CacheControl = `max-age=10, private=X-Llamas, private=Set-Cookie"`
	upstream.Header.Add("X-Llamas", "fully")
	upstream.Header.Add("Set-Cookie", "llamas=true")
	assert.Equal(t, http.StatusOK, client.get("/r1").Code)

	r1 := client.get("/r1")
	assert.Equal(t, http.StatusOK, r1.statusCode)
	assert.Equal(t, "HIT", r1.cacheStatus)
	assert.Equal(t, "fully", r1.HeaderMap.Get("X-Llamas"))
	assert.Equal(t, "llamas=true", r1.HeaderMap.Get("Set-Cookie"))
	assert.Equal(t, 1, upstream.requests)

	client.cacheHandler.Shared = true
	assert.Equal(t, http.StatusOK, client.get("/r2").Code)

	r2 := client.get("/r2")
	assert.Equal(t, http.StatusOK, r1.statusCode)
	assert.Equal(t, "HIT", r2.cacheStatus)
	assert.Equal(t, "", r2.HeaderMap.Get("X-Llamas"))
	assert.Equal(t, "", r2.HeaderMap.Get("Set-Cookie"))
	assert.Equal(t, 2, upstream.requests)
}

func TestSpecResponseCacheControlWithAuthorizationHeaders(t *testing.T) {
	client, upstream := testSetup()
	client.cacheHandler.Shared = true
	upstream.CacheControl = `max-age=10`
	upstream.Header.Add("Authorization", "fully")
	assert.Equal(t, http.StatusOK, client.get("/r1").Code)

	r1 := client.get("/r1")
	assert.Equal(t, http.StatusOK, r1.statusCode)
	assert.Equal(t, "SKIP", r1.cacheStatus)
	assert.Equal(t, "fully", r1.HeaderMap.Get("Authorization"))
	assert.Equal(t, 2, upstream.requests)

	client.cacheHandler.Shared = false
	assert.Equal(t, http.StatusOK, client.get("/r2").Code)

	r3 := client.get("/r2")
	assert.Equal(t, http.StatusOK, r3.statusCode)
	assert.Equal(t, "HIT", r3.cacheStatus)
	assert.Equal(t, "fully", r3.HeaderMap.Get("Authorization"))
	assert.Equal(t, 3, upstream.requests)
}

func TestSpecRequestCacheControl(t *testing.T) {
	var cases = []struct {
		cacheControl   string
		cacheStatus    string
		requests       int
		secondsElapsed time.Duration
	}{
		{cacheControl: "", requests: 1},
		{cacheControl: "no-cache", requests: 2},
		{cacheControl: "no-store", requests: 2},
		{cacheControl: "max-age=0", requests: 2},
		{cacheControl: "max-stale", requests: 1, secondsElapsed: 65},
		{cacheControl: "max-stale=0", requests: 2, secondsElapsed: 65},
		{cacheControl: "max-stale=60", requests: 1, secondsElapsed: 65},
		{cacheControl: "max-stale=60", requests: 1, secondsElapsed: 65},
		{cacheControl: "max-age=30", requests: 2, secondsElapsed: 40},
		{cacheControl: "min-fresh=5", requests: 1},
		{cacheControl: "min-fresh=120", requests: 2},
	}

	for idx, c := range cases {
		client, upstream := testSetup()
		upstream.CacheControl = "max-age=60"

		assert.Equal(t, http.StatusOK, client.get("/").Code)
		upstream.timeTravel(time.Second * time.Duration(c.secondsElapsed))

		r := client.get("/", "Cache-Control: "+c.cacheControl)
		assert.Equal(t, http.StatusOK, r.statusCode)
		assert.Equal(t, c.requests, upstream.requests,
			fmt.Sprintf("case #%d failed, %+v", idx+1, c))
	}
}

func TestSpecRequestCacheControlWithOnlyIfCached(t *testing.T) {
	client, upstream := testSetup()
	upstream.CacheControl = "max-age=10"

	assert.Equal(t, http.StatusOK, client.get("/").Code)
	assert.Equal(t, http.StatusOK, client.get("/").Code)

	upstream.timeTravel(time.Second * 20)
	assert.Equal(t, http.StatusGatewayTimeout,
		client.get("/", "Cache-Control: only-if-cached").Code)

	assert.Equal(t, 1, upstream.requests)
}

func TestSpecCachingStatusCodes(t *testing.T) {
	client, upstream := testSetup()
	upstream.StatusCode = http.StatusNotFound
	upstream.CacheControl = "public, max-age=60"

	r1 := client.get("/r1")
	assert.Equal(t, http.StatusNotFound, r1.statusCode)
	assert.Equal(t, "MISS", r1.cacheStatus)
	assert.Equal(t, string(upstream.Body), string(r1.body))

	upstream.timeTravel(time.Second * 10)
	r2 := client.get("/r1")
	assert.Equal(t, http.StatusNotFound, r2.statusCode)
	assert.Equal(t, "HIT", r2.cacheStatus)
	assert.Equal(t, string(upstream.Body), string(r2.body))
	assert.Equal(t, time.Second*10, r2.age)

	upstream.StatusCode = http.StatusPaymentRequired
	r3 := client.get("/r2")
	assert.Equal(t, http.StatusPaymentRequired, r3.statusCode)
	assert.Equal(t, "SKIP", r3.cacheStatus)
}

func TestSpecConditionalCaching(t *testing.T) {
	client, upstream := testSetup()
	upstream.Etag = `"llamas"`

	r1 := client.get("/")
	assert.Equal(t, "MISS", r1.cacheStatus)
	assert.Equal(t, string(upstream.Body), string(r1.body))

	r2 := client.get("/", `If-None-Match: "llamas"`)
	assert.Equal(t, http.StatusNotModified, r2.Code)
	assert.Equal(t, "", string(r2.body))
	assert.Equal(t, "HIT", r2.cacheStatus)
}

func TestSpecRangeRequests(t *testing.T) {
	client, upstream := testSetup()

	r1 := client.get("/", "Range: bytes=0-3")
	assert.Equal(t, http.StatusPartialContent, r1.Code)
	assert.Equal(t, "SKIP", r1.cacheStatus)
	assert.Equal(t, string(upstream.Body[0:4]), string(r1.body))
}

func TestSpecHeuristicCaching(t *testing.T) {
	client, upstream := testSetup()
	upstream.LastModified = upstream.Now.AddDate(-1, 0, 0)
	assert.Equal(t, "MISS", client.get("/").cacheStatus)

	upstream.timeTravel(time.Hour * 48)
	r2 := client.get("/")
	assert.Equal(t, "HIT", r2.cacheStatus)
	assert.Equal(t, []string{"113 - \"Heuristic Expiration\""}, r2.Header()["Warning"])
	assert.Equal(t, 1, upstream.requests, "The second request shouldn't validate")
}

func TestSpecCacheControlTrumpsExpires(t *testing.T) {
	client, upstream := testSetup()
	upstream.LastModified = upstream.Now.AddDate(-1, 0, 0)
	upstream.CacheControl = "max-age=2"
	assert.Equal(t, "MISS", client.get("/").cacheStatus)
	assert.Equal(t, "HIT", client.get("/").cacheStatus)
	assert.Equal(t, 1, upstream.requests)

	upstream.timeTravel(time.Hour * 48)
	assert.Equal(t, "HIT", client.get("/").cacheStatus)
	assert.Equal(t, 2, upstream.requests)
}

func TestSpecNotCachedWithoutValidatorOrExpiration(t *testing.T) {
	client, upstream := testSetup()
	upstream.LastModified = time.Time{}
	upstream.Etag = ""

	assert.Equal(t, "SKIP", client.get("/").cacheStatus)
	assert.Equal(t, "SKIP", client.get("/").cacheStatus)
	assert.Equal(t, 2, upstream.requests)
}

func TestSpecNoCachingForInvalidExpires(t *testing.T) {
	client, upstream := testSetup()
	upstream.LastModified = time.Time{}
	upstream.Header.Set("Expires", "-1")

	assert.Equal(t, "SKIP", client.get("/").cacheStatus)
}

func TestSpecRequestsWithoutHostHeader(t *testing.T) {
	client, _ := testSetup()

	r := newRequest("GET", "http://example.org")
	r.Header.Del("Host")
	r.Host = ""

	resp := client.do(r)
	assert.Equal(t, http.StatusBadRequest, resp.Code,
		"Requests without a Host header should result in a 400")
}

func TestSpecCacheControlMaxStale(t *testing.T) {
	client, upstream := testSetup()
	upstream.CacheControl = "max-age=60"
	assert.Equal(t, "MISS", client.get("/").cacheStatus)

	upstream.timeTravel(time.Second * 90)
	upstream.Body = []byte("brand new content")
	r2 := client.get("/", "Cache-Control: max-stale=3600")
	assert.Equal(t, "HIT", r2.cacheStatus)
	assert.Equal(t, time.Second*90, r2.age)

	upstream.timeTravel(time.Second * 90)
	r3 := client.get("/")
	assert.Equal(t, "MISS", r3.cacheStatus)
	assert.Equal(t, time.Duration(0), r3.age)
}

func TestSpecValidatingStaleResponsesUnchanged(t *testing.T) {
	client, upstream := testSetup()
	upstream.CacheControl = "max-age=60"
	upstream.Etag = "llamas1"
	assert.Equal(t, "MISS", client.get("/").cacheStatus)

	upstream.timeTravel(time.Second * 90)
	upstream.Header.Add("X-New-Header", "1")

	r2 := client.get("/")
	assert.Equal(t, http.StatusOK, r2.Code)
	assert.Equal(t, string(upstream.Body), string(r2.body))
	assert.Equal(t, "HIT", r2.cacheStatus)
}

func TestSpecValidatingStaleResponsesWithNewContent(t *testing.T) {
	client, upstream := testSetup()
	upstream.CacheControl = "max-age=60"
	assert.Equal(t, "MISS", client.get("/").cacheStatus)

	upstream.timeTravel(time.Second * 90)
	upstream.Body = []byte("brand new content")

	r2 := client.get("/")
	assert.Equal(t, http.StatusOK, r2.Code)
	assert.Equal(t, "MISS", r2.cacheStatus)
	assert.Equal(t, "brand new content", string(r2.body))
	assert.Equal(t, time.Duration(0), r2.age)
}

func TestSpecValidatingStaleResponsesWithNewEtag(t *testing.T) {
	client, upstream := testSetup()
	upstream.CacheControl = "max-age=60"
	upstream.Etag = "llamas1"

	assert.Equal(t, "MISS", client.get("/").cacheStatus)

	upstream.timeTravel(time.Second * 90)
	upstream.Etag = "llamas2"

	r2 := client.get("/")
	assert.Equal(t, http.StatusOK, r2.Code)
	assert.Equal(t, "MISS", r2.cacheStatus)
}

func TestSpecVaryHeader(t *testing.T) {
	client, upstream := testSetup()
	upstream.CacheControl = "max-age=60"
	upstream.Vary = "Accept-Language"
	upstream.Etag = "llamas"

	assert.Equal(t, "MISS", client.get("/", "Accept-Language: en").cacheStatus)
	assert.Equal(t, "HIT", client.get("/", "Accept-Language: en").cacheStatus)
	assert.Equal(t, "MISS", client.get("/", "Accept-Language: de").cacheStatus)
	assert.Equal(t, "HIT", client.get("/", "Accept-Language: de").cacheStatus)
}

func TestSpecHeadersPropagated(t *testing.T) {
	client, upstream := testSetup()
	upstream.CacheControl = "max-age=60"
	upstream.Header.Add("X-Llamas", "1")
	upstream.Header.Add("X-Llamas", "3")
	upstream.Header.Add("X-Llamas", "2")

	assert.Equal(t, "MISS", client.get("/").cacheStatus)

	r2 := client.get("/")
	assert.Equal(t, "HIT", r2.cacheStatus)
	assert.Equal(t, []string{"1", "3", "2"}, r2.Header()["X-Llamas"])
}

func TestSpecAgeHeaderFromUpstream(t *testing.T) {
	client, upstream := testSetup()
	upstream.CacheControl = "max-age=86400"
	upstream.Header.Set("Age", "3600") //1hr
	assert.Equal(t, time.Hour, client.get("/").age)

	upstream.timeTravel(time.Hour * 2)
	assert.Equal(t, time.Hour*3, client.get("/").age)
}

func TestSpecAgeHeaderWithResponseDelay(t *testing.T) {
	client, upstream := testSetup()
	upstream.CacheControl = "max-age=86400"
	upstream.Header.Set("Age", "3600") //1hr
	upstream.ResponseDuration = time.Second * 2
	assert.Equal(t, time.Second*3602, client.get("/").age)

	upstream.timeTravel(time.Second * 60)
	assert.Equal(t, time.Second*3662, client.get("/").age)
	assert.Equal(t, 1, upstream.requests)
}

func TestSpecAgeHeaderGeneratedWhereNoneExists(t *testing.T) {
	client, upstream := testSetup()
	upstream.CacheControl = "max-age=86400"
	upstream.ResponseDuration = time.Second * 2
	assert.Equal(t, time.Second*2, client.get("/").age)

	upstream.timeTravel(time.Second * 60)
	assert.Equal(t, time.Second*62, client.get("/").age)
	assert.Equal(t, 1, upstream.requests)
}

func TestSpecWarningForOldContent(t *testing.T) {
	client, upstream := testSetup()
	upstream.LastModified = upstream.Now.AddDate(-1, 0, 0)
	assert.Equal(t, "MISS", client.get("/").cacheStatus)

	upstream.timeTravel(time.Hour * 48)
	r2 := client.get("/")
	assert.Equal(t, "HIT", r2.cacheStatus)
	assert.Equal(t, []string{"113 - \"Heuristic Expiration\""}, r2.Header()["Warning"])
}

func TestSpecHeadCanBeServedFromCacheOnlyWithExplicitFreshness(t *testing.T) {
	client, upstream := testSetup()
	upstream.CacheControl = "max-age=3600"
	assert.Equal(t, "MISS", client.get("/explicit").cacheStatus)
	assert.Equal(t, "HIT", client.head("/explicit").cacheStatus)
	assert.Equal(t, "HIT", client.head("/explicit").cacheStatus)

	upstream.CacheControl = ""
	assert.Equal(t, "SKIP", client.get("/implicit").cacheStatus)
	assert.Equal(t, "SKIP", client.head("/implicit").cacheStatus)
	assert.Equal(t, "SKIP", client.head("/implicit").cacheStatus)
}

func TestSpecInvalidatingGetWithHeadRequest(t *testing.T) {
	client, upstream := testSetup()
	upstream.CacheControl = "max-age=3600"
	assert.Equal(t, "MISS", client.get("/explicit").cacheStatus)

	upstream.Body = []byte("brand new content")
	assert.Equal(t, "SKIP", client.head("/explicit", "Cache-Control: max-age=0").cacheStatus)
	assert.Equal(t, "MISS", client.get("/explicit").cacheStatus)
}

func TestSpecFresheningGetWithHeadRequest(t *testing.T) {
	client, upstream := testSetup()
	upstream.CacheControl = "max-age=3600"
	assert.Equal(t, "MISS", client.get("/explicit").cacheStatus)

	upstream.timeTravel(time.Second * 10)
	assert.Equal(t, 10*time.Second, client.get("/explicit").age)

	upstream.Header.Add("X-Llamas", "llamas")
	assert.Equal(t, "SKIP", client.head("/explicit", "Cache-Control: max-age=0").cacheStatus)

	refreshed := client.get("/explicit")
	assert.Equal(t, "HIT", refreshed.cacheStatus)
	assert.Equal(t, time.Duration(0), refreshed.age)
	assert.Equal(t, "llamas", refreshed.header.Get("X-Llamas"))
}

func TestSpecContentHeaderInRequestRespected(t *testing.T) {
	client, upstream := testSetup()
	upstream.CacheControl = "max-age=3600"

	r1 := client.get("/llamas/rock")
	assert.Equal(t, "MISS", r1.cacheStatus)
	assert.Equal(t, string(upstream.Body), string(r1.body))

	r2 := client.get("/another/llamas", "Content-Location: /llamas/rock")
	assert.Equal(t, "HIT", r2.cacheStatus)
	assert.Equal(t, string(upstream.Body), string(r2.body))
}

func TestSpecMultipleCacheControlHeaders(t *testing.T) {
	client, upstream := testSetup()
	upstream.Header.Add("Cache-Control", "max-age=60, max-stale=10")
	upstream.Header.Add("Cache-Control", "no-cache")

	r1 := client.get("/")
	assert.Equal(t, "SKIP", r1.cacheStatus)
}
