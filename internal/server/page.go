package server

import (
	_ "embed"
	"strings"
)

//go:embed templates/default.html
var server_default_template string

func GetBaseTemplate(cacheSize string, filesNumber string, availableSize string,
	memoryUsage string, goroutines string) string {

	tpl := strings.Replace(server_default_template, "$APT_PROXY_CACHE_SIZE", cacheSize, 1)
	tpl = strings.Replace(tpl, "$APT_PROXY_FILE_NUMBER", filesNumber, 1)
	tpl = strings.Replace(tpl, "$APT_PROXY_AVAILABLE_SIZE", availableSize, 1)
	tpl = strings.Replace(tpl, "$APT_PROXY_MEMORY_USAGE", memoryUsage, 1)
	tpl = strings.Replace(tpl, "$APT_PROXY_GOROUTINES", goroutines, 1)

	return tpl
}
