package define

import (
	"bytes"
	"fmt"
	"regexp"
	"strings"
	"text/template"
)

const (
	LINUX_ALL_DISTROS          string = "all"
	LINUX_DISTROS_UBUNTU       string = "ubuntu"
	LINUX_DISTROS_UBUNTU_PORTS string = "ubuntu-ports"
	LINUX_DISTROS_DEBIAN       string = "debian"
	LINUX_DISTROS_CENTOS       string = "centos"
	LINUX_DISTROS_ALPINE       string = "alpine"
)

const (
	TYPE_LINUX_ALL_DISTROS          int = 0
	TYPE_LINUX_DISTROS_UBUNTU       int = 1
	TYPE_LINUX_DISTROS_UBUNTU_PORTS int = 2
	TYPE_LINUX_DISTROS_DEBIAN       int = 3
	TYPE_LINUX_DISTROS_CENTOS       int = 4
	TYPE_LINUX_DISTROS_ALPINE       int = 5
)

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

type UrlWithAlias struct {
	URL       string
	Alias     string
	Http      bool
	Https     bool
	Official  bool
	Bandwidth int64
}

func GenerateAliasFromURL(url string) string {
	pureHost := regexp.MustCompile(`^https?://|\/.*`).ReplaceAllString(url, "")
	tldRemoved := regexp.MustCompile(`\.edu\.cn$|.cn$|\.com$|\.net$|\.net.cn$|\.org$|\.org\.cn$`).ReplaceAllString(pureHost, "")
	group := strings.Split(tldRemoved, ".")
	alias := group[len(group)-1]

	// Use templates for alias construction
	var buf bytes.Buffer
	data := AliasTemplateData{Alias: alias}
	if err := aliasTemplate.Execute(&buf, data); err != nil {
		// Fallback to concatenation if template fails
		return "cn:" + alias
	}
	return buf.String()
}

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
	// TODO
	mirror.Bandwidth = 0
	return mirror
}

var (
	// httpURLTemplate is a template for constructing HTTP URLs
	httpURLTemplate = template.Must(template.New("httpURL").Parse("http://{{.URL}}"))

	// httpsURLTemplate is a template for constructing HTTPS URLs
	httpsURLTemplate = template.Must(template.New("httpsURL").Parse("https://{{.URL}}"))

	// aliasTemplate is a template for constructing aliases
	aliasTemplate = template.Must(template.New("alias").Parse("cn:{{.Alias}}"))
)

// URLTemplateData holds data for URL template execution
type URLTemplateData struct {
	URL string
}

// AliasTemplateData holds data for alias template execution
type AliasTemplateData struct {
	Alias string
}

// buildHTTPURL constructs an HTTP URL using templates
func buildHTTPURL(url string) (string, error) {
	var buf bytes.Buffer
	data := URLTemplateData{URL: url}
	if err := httpURLTemplate.Execute(&buf, data); err != nil {
		return "", err
	}
	return buf.String(), nil
}

// buildHTTPSURL constructs an HTTPS URL using templates
func buildHTTPSURL(url string) (string, error) {
	var buf bytes.Buffer
	data := URLTemplateData{URL: url}
	if err := httpsURLTemplate.Execute(&buf, data); err != nil {
		return "", err
	}
	return buf.String(), nil
}

func GenerateBuildInList(officialList []string, customList []string) (mirrors []UrlWithAlias) {
	for _, url := range officialList {
		if !strings.HasPrefix(url, "http://") && !strings.HasPrefix(url, "https://") {
			httpURL, err := buildHTTPURL(url)
			if err != nil {
				// Fallback to concatenation if template fails
				httpURL = "http://" + url
			}
			mirror := GenerateBuildInMirorItem(httpURL, true)
			mirrors = append(mirrors, mirror)

			httpsURL, err := buildHTTPSURL(url)
			if err != nil {
				// Fallback to concatenation if template fails
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
				// Fallback to concatenation if template fails
				httpURL = "http://" + url
			}
			mirror := GenerateBuildInMirorItem(httpURL, false)
			mirrors = append(mirrors, mirror)

			httpsURL, err := buildHTTPSURL(url)
			if err != nil {
				// Fallback to concatenation if template fails
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
