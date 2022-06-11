package linux

import (
	"fmt"
	"log"
	"net/http"
	"net/url"
	"regexp"
)

type URLRewriter struct {
	mirror  *url.URL
	pattern *regexp.Regexp
}

func NewRewriter(mirror string, osType string) *URLRewriter {
	u := &URLRewriter{}

	if len(mirror) > 0 {
		mirrorUrl, err := url.Parse(mirror)
		if err == nil {
			log.Printf("using ubuntu mirror %s", mirror)
			u.mirror = mirrorUrl
			_, _, pattern := getPredefinedConfiguration(osType)
			u.pattern = pattern
			return u
		}
	}

	// benchmark in the background to make sure we have the fastest
	go func() {
		mirrorsListUrl, benchmarkUrl, pattern := getPredefinedConfiguration(osType)
		u.pattern = pattern

		mirrors, err := getGeoMirrors(mirrorsListUrl)
		if err != nil {
			log.Fatal(err)
		}

		mirror, err := fastest(mirrors, benchmarkUrl)
		if err != nil {
			log.Println("Error finding fastest mirror", err)
		}

		if mirrorUrl, err := url.Parse(mirror); err == nil {
			log.Printf("using ubuntu mirror %s", mirror)
			u.mirror = mirrorUrl
		}
	}()

	return u
}

func Rewrite(r *http.Request, rewriter *URLRewriter) {
	uri := r.URL.String()
	if rewriter.mirror != nil && rewriter.pattern.MatchString(uri) {
		r.Header.Add("Content-Location", uri)
		m := rewriter.pattern.FindAllStringSubmatch(uri, -1)
		// Fix the problem of double escaping of symbols
		unescapedQuery, err := url.PathUnescape(m[0][2])
		if err != nil {
			unescapedQuery = m[0][2]
		}
		r.URL.Host = rewriter.mirror.Host
		r.URL.Path = rewriter.mirror.Path + unescapedQuery
	}
}

func MatchingRule(subject string, rules []Rule) (*Rule, bool) {
	for _, rule := range rules {
		if rule.Pattern.MatchString(subject) {
			return &rule, true
		}
	}
	return nil, false
}

func (r *Rule) String() string {
	return fmt.Sprintf("%s Cache-Control=%s Rewrite=%#v",
		r.Pattern.String(), r.CacheControl, r.Rewrite)
}
