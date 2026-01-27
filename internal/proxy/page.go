package proxy

import (
	"bytes"
	"embed"
	"html/template"
	"sync"
)

//go:embed templates/home.html
var templateFS embed.FS

// HomePageData holds the data for rendering the home page template
type HomePageData struct {
	CacheSize     string
	FilesNumber   string
	AvailableSize string
	MemoryUsage   string
	Goroutines    string
}

var (
	homeTemplate     *template.Template
	homeTemplateOnce sync.Once
	homeTemplateErr  error
)

// getHomeTemplate returns the parsed home page template.
// It uses sync.Once to ensure the template is parsed only once.
func getHomeTemplate() (*template.Template, error) {
	homeTemplateOnce.Do(func() {
		content, err := templateFS.ReadFile("templates/home.html")
		if err != nil {
			homeTemplateErr = err
			return
		}
		homeTemplate, homeTemplateErr = template.New("home").Parse(string(content))
	})
	return homeTemplate, homeTemplateErr
}

// GetBaseTemplate renders the home page template with the provided statistics.
// It uses html/template for safe rendering and returns the rendered HTML string.
func GetBaseTemplate(cacheSize string, filesNumber string, availableSize string,
	memoryUsage string, goroutines string) string {

	tmpl, err := getHomeTemplate()
	if err != nil {
		// Fallback to a simple error page if template loading fails
		return getErrorPage(err)
	}

	data := HomePageData{
		CacheSize:     cacheSize,
		FilesNumber:   filesNumber,
		AvailableSize: availableSize,
		MemoryUsage:   memoryUsage,
		Goroutines:    goroutines,
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return getErrorPage(err)
	}

	return buf.String()
}

// getErrorPage returns a simple error page HTML when template rendering fails
func getErrorPage(err error) string {
	return `<!DOCTYPE html>
<html lang="en">
<head>
    <meta charset="UTF-8">
    <title>APT Proxy - Error</title>
    <style>
        body {
            background: #343434;
            color: #fff;
            font-family: sans-serif;
            display: flex;
            justify-content: center;
            align-items: center;
            height: 100vh;
            margin: 0;
        }
        .error {
            text-align: center;
            padding: 40px;
            background: rgba(255,255,255,0.1);
            border-radius: 8px;
        }
        h1 { color: #f35153; }
    </style>
</head>
<body>
    <div class="error">
        <h1>Template Error</h1>
        <p>Failed to render the home page template.</p>
        <p><a href="https://github.com/soulteary/apt-proxy" style="color: #3cabee;">GitHub Repository</a></p>
    </div>
</body>
</html>`
}
