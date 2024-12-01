package rewriter

import (
	"log"
	"net/http"
	"net/url"
	"regexp"

	Define "github.com/soulteary/apt-proxy/define"
	Benchmark "github.com/soulteary/apt-proxy/internal/benchmark"
	Mirrors "github.com/soulteary/apt-proxy/internal/mirrors"
	State "github.com/soulteary/apt-proxy/internal/state"
)

type URLRewriters struct {
	ubuntu *URLRewriter
	debian *URLRewriter
	centos *URLRewriter
	alpine *URLRewriter
}

type URLRewriter struct {
	mirror  *url.URL
	pattern *regexp.Regexp
}

func GetRewriteRulesByMode(mode int) (rules []Define.Rule) {
	if mode == Define.TYPE_LINUX_DISTROS_UBUNTU {
		return Define.UBUNTU_DEFAULT_CACHE_RULES
	}
	if mode == Define.TYPE_LINUX_DISTROS_DEBIAN {
		return Define.DEBIAN_DEFAULT_CACHE_RULES
	}
	if mode == Define.TYPE_LINUX_DISTROS_CENTOS {
		return Define.CENTOS_DEFAULT_CACHE_RULES
	}
	if mode == Define.TYPE_LINUX_DISTROS_ALPINE {
		return Define.ALPINE_DEFAULT_CACHE_RULES
	}

	rules = append(rules, Define.UBUNTU_DEFAULT_CACHE_RULES...)
	rules = append(rules, Define.DEBIAN_DEFAULT_CACHE_RULES...)
	rules = append(rules, Define.CENTOS_DEFAULT_CACHE_RULES...)
	rules = append(rules, Define.ALPINE_DEFAULT_CACHE_RULES...)
	return rules
}

func getRewriterForAlpine() *URLRewriter {
	u := &URLRewriter{}
	mirror := State.GetAlpineMirror()
	benchmarkUrl, pattern := Mirrors.GetPredefinedConfiguration(Define.TYPE_LINUX_DISTROS_ALPINE)
	u.pattern = pattern

	if mirror != nil {
		log.Printf("using specify Alpine mirror %s", mirror)
		u.mirror = mirror
		return u
	}

	mirrors := Mirrors.GetGeoMirrorUrlsByMode(Define.TYPE_LINUX_DISTROS_ALPINE)
	fastest, err := Benchmark.GetTheFastestMirror(mirrors, benchmarkUrl)
	if err != nil {
		log.Println("Error finding fastest Alpine mirror", err)
	}

	if mirror, err := url.Parse(fastest); err == nil {
		log.Printf("using fastest Alpine mirror %s", fastest)
		u.mirror = mirror
	}

	return u
}

func getRewriterForCentOS() *URLRewriter {
	u := &URLRewriter{}
	mirror := State.GetCentOSMirror()
	benchmarkUrl, pattern := Mirrors.GetPredefinedConfiguration(Define.TYPE_LINUX_DISTROS_CENTOS)
	u.pattern = pattern

	if mirror != nil {
		log.Printf("using specify CentOS mirror %s", mirror)
		u.mirror = mirror
		return u
	}

	mirrors := Mirrors.GetGeoMirrorUrlsByMode(Define.TYPE_LINUX_DISTROS_CENTOS)
	fastest, err := Benchmark.GetTheFastestMirror(mirrors, benchmarkUrl)
	if err != nil {
		log.Println("Error finding fastest CentOS mirror", err)
	}

	if mirror, err := url.Parse(fastest); err == nil {
		log.Printf("using fastest CentOS mirror %s", fastest)
		u.mirror = mirror
	}

	return u
}

func getRewriterForDebian() *URLRewriter {
	u := &URLRewriter{}
	mirror := State.GetDebianMirror()
	benchmarkUrl, pattern := Mirrors.GetPredefinedConfiguration(Define.TYPE_LINUX_DISTROS_DEBIAN)
	u.pattern = pattern

	if mirror != nil {
		log.Printf("using specify Debian mirror %s", mirror)
		u.mirror = mirror
		return u
	}

	mirrors := Mirrors.GetGeoMirrorUrlsByMode(Define.TYPE_LINUX_DISTROS_DEBIAN)
	fastest, err := Benchmark.GetTheFastestMirror(mirrors, benchmarkUrl)
	if err != nil {
		log.Println("Error finding fastest Debian mirror", err)
	}

	if mirror, err := url.Parse(fastest); err == nil {
		log.Printf("using fastest Debian mirror %s", fastest)
		u.mirror = mirror
	}

	return u
}

func getRewriterForUbuntu() *URLRewriter {
	u := &URLRewriter{}
	mirror := State.GetUbuntuMirror()
	benchmarkUrl, pattern := Mirrors.GetPredefinedConfiguration(Define.TYPE_LINUX_DISTROS_UBUNTU)
	u.pattern = pattern

	if mirror != nil {
		log.Printf("using specify Ubuntu mirror %s", mirror)
		u.mirror = mirror
		return u
	}

	mirrors := Mirrors.GetGeoMirrorUrlsByMode(Define.TYPE_LINUX_DISTROS_UBUNTU)
	fastest, err := Benchmark.GetTheFastestMirror(mirrors, benchmarkUrl)
	if err != nil {
		log.Println("Error finding fastest Ubuntu mirror", err)
	}

	if mirror, err := url.Parse(fastest); err == nil {
		log.Printf("using fastest Ubuntu mirror %s", fastest)
		u.mirror = mirror
	}

	return u
}

func CreateNewRewriters(mode int) *URLRewriters {
	rewriters := &URLRewriters{}

	if mode == Define.TYPE_LINUX_DISTROS_UBUNTU {
		rewriters.ubuntu = getRewriterForUbuntu()
		return rewriters
	}

	if mode == Define.TYPE_LINUX_DISTROS_DEBIAN {
		rewriters.debian = getRewriterForDebian()
		return rewriters
	}

	if mode == Define.TYPE_LINUX_DISTROS_CENTOS {
		rewriters.centos = getRewriterForCentOS()
		return rewriters
	}

	if mode == Define.TYPE_LINUX_DISTROS_ALPINE {
		rewriters.alpine = getRewriterForAlpine()
		return rewriters
	}

	rewriters.ubuntu = getRewriterForUbuntu()
	rewriters.debian = getRewriterForDebian()
	rewriters.centos = getRewriterForCentOS()
	rewriters.alpine = getRewriterForAlpine()
	return rewriters
}

func RewriteRequestByMode(r *http.Request, rewriters *URLRewriters, mode int) {
	uri := r.URL.String()

	var rewriter *URLRewriter
	switch mode {
	case Define.TYPE_LINUX_DISTROS_UBUNTU:
		rewriter = rewriters.ubuntu
	case Define.TYPE_LINUX_DISTROS_DEBIAN:
		rewriter = rewriters.debian
	case Define.TYPE_LINUX_DISTROS_CENTOS:
		rewriter = rewriters.centos
	case Define.TYPE_LINUX_DISTROS_ALPINE:
		rewriter = rewriters.alpine
	}

	if rewriter.mirror != nil && rewriter.pattern.MatchString(uri) {
		r.Header.Add("Content-Location", uri)
		m := rewriter.pattern.FindAllStringSubmatch(uri, -1)
		// Fix the problem of double escaping of symbols
		queryRaw := m[0][len(m[0])-1]
		unescapedQuery, err := url.PathUnescape(queryRaw)
		if err != nil {
			unescapedQuery = queryRaw
		}
		r.URL.Scheme = rewriter.mirror.Scheme
		r.URL.Host = rewriter.mirror.Host
		r.URL.Path = rewriter.mirror.Path + unescapedQuery
	}
}

func MatchingRule(subject string, rules []Define.Rule) (*Define.Rule, bool) {
	for _, rule := range rules {
		if rule.Pattern.MatchString(subject) {
			return &rule, true
		}
	}
	return nil, false
}
