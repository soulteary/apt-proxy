// Package logsafe prepares repository URLs for diagnostic logging.
package logsafe

import (
	"errors"
	"net/url"
	"strings"
)

// URL retains the origin, path and query keys while hiding credentials and all
// query values. Custom repository authentication can use arbitrary query names.
// Parse failures are redacted completely because they can contain credentials.
// The caller's routing URL is never modified.
func URL(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || u.Opaque != "" {
		return "[invalid repository URL redacted]"
	}
	if u.RawQuery != "" {
		u.RawQuery = Query(u.RawQuery)
	}
	if u.Fragment != "" {
		u.Fragment = "[redacted]"
		u.RawFragment = ""
	}
	return u.Redacted()
}

// Query keeps query keys for diagnostics but never logs their values.
func Query(raw string) string {
	if raw == "" {
		return ""
	}
	query, err := url.ParseQuery(raw)
	if err != nil {
		return "[query redacted]"
	}
	for key := range query {
		query[key] = []string{"[redacted]"}
	}
	return query.Encode()
}

// Error hides URLs inside HTTP request/parse errors, including wrapped and
// joined errors from mirror election. Use only at logging boundaries so the
// original error and its classification remain available to callers.
func Error(err error) error {
	if err == nil {
		return nil
	}
	return errors.New(errorText(err))
}

func errorText(err error) string {
	if e, ok := err.(*url.Error); ok {
		return (&url.Error{Op: e.Op, URL: URL(e.URL), Err: Error(e.Err)}).Error()
	}
	result := err.Error()
	switch e := err.(type) {
	case interface{ Unwrap() []error }:
		for _, child := range e.Unwrap() {
			if child != nil {
				result = strings.ReplaceAll(result, child.Error(), errorText(child))
			}
		}
	case interface{ Unwrap() error }:
		if child := e.Unwrap(); child != nil {
			result = strings.ReplaceAll(result, child.Error(), errorText(child))
		}
	}
	return result
}
