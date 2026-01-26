// Package distro provides distribution-specific definitions and caching rules
// for apt-proxy. This package contains constants, types, and configurations
// for supported Linux distributions (Ubuntu, Debian, CentOS, Alpine).
package distro

import (
	"bytes"
	"fmt"
	"regexp"
	"strings"
	"text/template"
)

// Distribution name constants
const (
	LINUX_ALL_DISTROS          string = "all"
	LINUX_DISTROS_UBUNTU       string = "ubuntu"
	LINUX_DISTROS_UBUNTU_PORTS string = "ubuntu-ports"
	LINUX_DISTROS_DEBIAN       string = "debian"
	LINUX_DISTROS_CENTOS       string = "centos"
	LINUX_DISTROS_ALPINE       string = "alpine"
)

// Distribution type constants
const (
	TYPE_LINUX_ALL_DISTROS          int = 0
	TYPE_LINUX_DISTROS_UBUNTU       int = 1
	TYPE_LINUX_DISTROS_UBUNTU_PORTS int = 2
	TYPE_LINUX_DISTROS_DEBIAN       int = 3
	TYPE_LINUX_DISTROS_CENTOS       int = 4
	TYPE_LINUX_DISTROS_ALPINE       int = 5
)

// Rule defines a caching rule for package files
type Rule struct {
	OS           int
	Pattern      *regexp.Regexp
	CacheControl string
	Rewrite      bool
}

func (r *Rule) String() string {
	return fmt.Sprintf("%s Cache-Control=%s Rewrite=%#v",
		r.Pattern.String(), r.CacheControl, r.Rewrite)
}

// UrlWithAlias represents a mirror URL with its alias and metadata
type UrlWithAlias struct {
	URL       string
	Alias     string
	Http      bool
	Https     bool
	Official  bool
	Bandwidth int64
}

// GenerateAliasFromURL generates an alias from a URL
func GenerateAliasFromURL(url string) string {
	pureHost := urlSchemeAndPathRegex.ReplaceAllString(url, "")
	tldRemoved := tldRemovalRegex.ReplaceAllString(pureHost, "")
	group := strings.Split(tldRemoved, ".")
	alias := group[len(group)-1]

	var buf bytes.Buffer
	data := AliasTemplateData{Alias: alias}
	if err := aliasTemplate.Execute(&buf, data); err != nil {
		return "cn:" + alias
	}
	return buf.String()
}

// GenerateBuildInMirorItem creates a UrlWithAlias from a URL
func GenerateBuildInMirorItem(url string, official bool) UrlWithAlias {
	var mirror UrlWithAlias
	mirror.Official = official
	mirror.Alias = GenerateAliasFromURL(url)

	if strings.HasPrefix(url, "http://") {
		mirror.Http = true
		mirror.Https = false
	} else if strings.HasPrefix(url, "https://") {
		mirror.Http = false
		mirror.Https = true
	}
	mirror.URL = url
	mirror.Bandwidth = 0
	return mirror
}

var (
	httpURLTemplate  = template.Must(template.New("httpURL").Parse("http://{{.URL}}"))
	httpsURLTemplate = template.Must(template.New("httpsURL").Parse("https://{{.URL}}"))
	aliasTemplate    = template.Must(template.New("alias").Parse("cn:{{.Alias}}"))

	urlSchemeAndPathRegex = regexp.MustCompile(`^https?://|\/.*`)
	tldRemovalRegex       = regexp.MustCompile(`\.edu\.cn$|\.cn$|\.com$|\.net$|\.net\.cn$|\.org$|\.org\.cn$`)
)

// URLTemplateData holds data for URL template execution
type URLTemplateData struct {
	URL string
}

// AliasTemplateData holds data for alias template execution
type AliasTemplateData struct {
	Alias string
}

func buildHTTPURL(url string) (string, error) {
	var buf bytes.Buffer
	data := URLTemplateData{URL: url}
	if err := httpURLTemplate.Execute(&buf, data); err != nil {
		return "", err
	}
	return buf.String(), nil
}

func buildHTTPSURL(url string) (string, error) {
	var buf bytes.Buffer
	data := URLTemplateData{URL: url}
	if err := httpsURLTemplate.Execute(&buf, data); err != nil {
		return "", err
	}
	return buf.String(), nil
}

// GenerateBuildInList generates a list of mirror URLs with aliases
func GenerateBuildInList(officialList []string, customList []string) (mirrors []UrlWithAlias) {
	for _, url := range officialList {
		if !strings.HasPrefix(url, "http://") && !strings.HasPrefix(url, "https://") {
			httpURL, err := buildHTTPURL(url)
			if err != nil {
				httpURL = "http://" + url
			}
			mirror := GenerateBuildInMirorItem(httpURL, true)
			mirrors = append(mirrors, mirror)

			httpsURL, err := buildHTTPSURL(url)
			if err != nil {
				httpsURL = "https://" + url
			}
			mirror = GenerateBuildInMirorItem(httpsURL, true)
			mirrors = append(mirrors, mirror)
		} else {
			mirror := GenerateBuildInMirorItem(url, true)
			mirrors = append(mirrors, mirror)
		}
	}

	for _, url := range customList {
		if !strings.HasPrefix(url, "http://") && !strings.HasPrefix(url, "https://") {
			httpURL, err := buildHTTPURL(url)
			if err != nil {
				httpURL = "http://" + url
			}
			mirror := GenerateBuildInMirorItem(httpURL, false)
			mirrors = append(mirrors, mirror)

			httpsURL, err := buildHTTPSURL(url)
			if err != nil {
				httpsURL = "https://" + url
			}
			mirror = GenerateBuildInMirorItem(httpsURL, false)
			mirrors = append(mirrors, mirror)
		} else {
			mirror := GenerateBuildInMirorItem(url, false)
			mirrors = append(mirrors, mirror)
		}
	}

	return mirrors
}
