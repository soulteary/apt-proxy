
# httpcache

`httpcache` provides an [rfc7234][] compliant golang [http.Handler](http://golang.org/pkg/net/http/#Handler).

[![wercker status](https://app.wercker.com/status/a76986990d27e72ea656bb37bb93f59f/m "wercker status")](https://app.wercker.com/project/bykey/a76986990d27e72ea656bb37bb93f59f)

[![GoDoc](https://godoc.org/github.com/lox/httpcache?status.svg)](https://godoc.org/github.com/lox/httpcache)

## Example

This example is from the included CLI, it runs a caching proxy on http://localhost:8080.

```go
proxy := &httputil.ReverseProxy{
    Director: func(r *http.Request) {
    },
}

handler := httpcache.NewHandler(httpcache.NewMemoryCache(), proxy)
handler.Shared = true

log.Printf("proxy listening on http://%s", listen)
log.Fatal(http.ListenAndServe(listen, handler))
```

## Implemented

- All of [rfc7234][], except those listed below
- Disk and Memory storage
- Apache-like logging via `httplog` package

## Todo

- Offline operation
- Size constraints on memory/disk cache and cache eviction 
- Correctly handle mixture of HTTP1.0 clients and 1.1 upstreams
- More detail in `Via` header
- Support for weak entities with `If-Match` and `If-None-Match`
- Invalidation based on `Content-Location` and request method
- Better handling of duplicate headers and CacheControl values

## Caveats

- Conditional requests are never cached, this includes `Range` requests

## Testing

Tests are currently conducted via the test suite and verified via the [CoAdvisor tool](http://coad.measurement-factory.com/).

## Reading List

- http://httpwg.github.io/specs/rfc7234.html
- https://www.mnot.net/blog/2011/07/11/what_proxies_must_do
- https://www.mnot.net/blog/2014/06/07/rfc2616_is_dead

[rfc7234]: http://httpwg.github.io/specs/rfc7234.html
