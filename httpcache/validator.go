package httpcache

import (
	"fmt"
	"net/http"
	"net/http/httptest"
)

type Validator struct {
	Handler http.Handler
}

func (v *Validator) Validate(req *http.Request, res *Resource) bool {
	outreq := cloneRequest(req)
	resHeaders := res.Header()

	if etag := resHeaders.Get("Etag"); etag != "" {
		outreq.Header.Set("If-None-Match", etag)
	} else if lastMod := resHeaders.Get("Last-Modified"); lastMod != "" {
		outreq.Header.Set("If-Modified-Since", lastMod)
	}

	t := Clock()
	resp := httptest.NewRecorder()
	v.Handler.ServeHTTP(resp, outreq)
	resp.Flush()

	if age, err := correctedAge(resp.HeaderMap, t, Clock()); err == nil {
		resp.Header().Set("Age", fmt.Sprintf("%.f", age.Seconds()))
	}

	if headersEqual(resHeaders, resp.HeaderMap) {
		res.header = resp.HeaderMap
		res.header.Set(ProxyDateHeader, Clock().Format(http.TimeFormat))
		return true
	}

	return false
}

var validationHeaders = []string{"ETag", "Content-MD5", "Last-Modified", "Content-Length"}

func headersEqual(h1, h2 http.Header) bool {
	for _, header := range validationHeaders {
		if value := h2.Get(header); value != "" {
			if h1.Get(header) != value {
				debugf("%s changed, %q != %q", header, value, h1.Get(header))
				return false
			}
		}
	}

	return true
}

// cloneRequest returns a clone of the provided *http.Request.
// The clone is a shallow copy of the struct and its Header map.
func cloneRequest(r *http.Request) *http.Request {
	r2 := new(http.Request)
	*r2 = *r
	r2.Header = make(http.Header)
	for k, s := range r.Header {
		r2.Header[k] = s
	}
	return r2
}
