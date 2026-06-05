package mirrors

import (
	"bytes"
	"text/template"
)

// URLTemplateData holds data for URL template execution
type URLTemplateData struct {
	URL string
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
