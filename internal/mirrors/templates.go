package mirrors

import (
	"bytes"
	"text/template"
)

// URLTemplateData holds data for URL template execution
type URLTemplateData struct {
	Scheme string
	Host   string
	Path   string
	Query  string
	URL    string
}

// ListenAddressTemplateData holds data for listen address template execution
type ListenAddressTemplateData struct {
	Host string
	Port string
}

var (
	// httpURLTemplate is a template for constructing HTTP URLs
	httpURLTemplate = template.Must(template.New("httpURL").Parse("http://{{.URL}}"))

	// httpsURLTemplate is a template for constructing HTTPS URLs
	httpsURLTemplate = template.Must(template.New("httpsURL").Parse("https://{{.URL}}"))

	// fullURLTemplate is a template for constructing full URLs with scheme, host, and path
	fullURLTemplate = template.Must(template.New("fullURL").Parse("{{.Scheme}}://{{.Host}}{{.Path}}{{.Query}}"))

	// listenAddressTemplate is a template for constructing listen addresses
	listenAddressTemplate = template.Must(template.New("listenAddress").Parse("{{.Host}}:{{.Port}}"))
)

// BuildHTTPURL constructs an HTTP URL using templates
func BuildHTTPURL(url string) (string, error) {
	var buf bytes.Buffer
	data := URLTemplateData{URL: url}
	if err := httpURLTemplate.Execute(&buf, data); err != nil {
		return "", err
	}
	return buf.String(), nil
}

// BuildHTTPSURL constructs an HTTPS URL using templates
func BuildHTTPSURL(url string) (string, error) {
	var buf bytes.Buffer
	data := URLTemplateData{URL: url}
	if err := httpsURLTemplate.Execute(&buf, data); err != nil {
		return "", err
	}
	return buf.String(), nil
}

// BuildFullURL constructs a full URL with scheme, host, path, and optional query using templates
func BuildFullURL(scheme, host, path, query string) (string, error) {
	var buf bytes.Buffer
	data := URLTemplateData{
		Scheme: scheme,
		Host:   host,
		Path:   path,
		Query:  query,
	}
	if err := fullURLTemplate.Execute(&buf, data); err != nil {
		return "", err
	}
	return buf.String(), nil
}

// BuildListenAddress constructs a listen address using templates
func BuildListenAddress(host, port string) (string, error) {
	var buf bytes.Buffer
	data := ListenAddressTemplateData{
		Host: host,
		Port: port,
	}
	if err := listenAddressTemplate.Execute(&buf, data); err != nil {
		return "", err
	}
	return buf.String(), nil
}

// PathTemplateData holds data for path template execution
type PathTemplateData struct {
	Path  string
	Query string
}

var (
	// pathQueryTemplate is a template for constructing paths with query strings
	pathQueryTemplate = template.Must(template.New("pathQuery").Parse("{{.Path}}{{.Query}}"))
)

// BuildPathWithQuery constructs a path with query using templates
func BuildPathWithQuery(path, query string) (string, error) {
	var buf bytes.Buffer
	data := PathTemplateData{
		Path:  path,
		Query: query,
	}
	if err := pathQueryTemplate.Execute(&buf, data); err != nil {
		return "", err
	}
	return buf.String(), nil
}
