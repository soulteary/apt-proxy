package mirrors

// URLTemplateData holds data for URL building (kept for API compatibility).
type URLTemplateData struct {
	URL string
}

// ListenAddressTemplateData holds data for listen address building (kept for
// API compatibility).
type ListenAddressTemplateData struct {
	Host string
	Port string
}

// BuildHTTPURL constructs an HTTP URL.
func BuildHTTPURL(url string) string {
	return "http://" + url
}

// BuildHTTPSURL constructs an HTTPS URL.
func BuildHTTPSURL(url string) string {
	return "https://" + url
}

// BuildListenAddress constructs a listen address (host:port).
func BuildListenAddress(host, port string) string {
	return host + ":" + port
}
