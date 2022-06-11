package linux

import (
	"log"
	"net/http"
	"net/url"
)

type URLRewriter struct {
	mirror *url.URL
}

func NewRewriter(mirror string) *URLRewriter {
	u := &URLRewriter{}

	if len(mirror) == 0 {
		// benchmark in the background to make sure we have the fastest
		go func() {
			mirrors, err := GetGeoMirrors()
			if err != nil {
				log.Fatal(err)
			}

			mirror, err := mirrors.Fastest()
			if err != nil {
				log.Println("Error finding fastest mirror", err)
			}

			if mirrorUrl, err := url.Parse(mirror); err == nil {
				log.Printf("using ubuntu mirror %s", mirror)
				u.mirror = mirrorUrl
			}
		}()
	} else {
		if mirrorUrl, err := url.Parse(mirror); err == nil {
			log.Printf("using ubuntu mirror %s", mirror)
			u.mirror = mirrorUrl
		}
	}
	return u
}

func (ur *URLRewriter) Rewrite(r *http.Request) {
	uri := r.URL.String()
	if ur.mirror != nil && hostPattern.MatchString(uri) {
		r.Header.Add("Content-Location", uri)
		m := hostPattern.FindAllStringSubmatch(uri, -1)
		// Fix the problem of double escaping of symbols
		unescapedQuery, err := url.PathUnescape(m[0][2])
		if err != nil {
			unescapedQuery = m[0][2]
		}
		r.URL.Host = ur.mirror.Host
		r.URL.Path = ur.mirror.Path + unescapedQuery
	}
}
