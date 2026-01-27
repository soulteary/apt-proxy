// Package errors provides unified error handling for apt-proxy.
// It defines standard error codes, structured error types, and helper functions
// for consistent error management across the application.
package errors

import (
	"encoding/json"
	"fmt"
	"net/http"
)

// Code represents a unique error code for categorizing errors.
type Code string

// Error codes for different error categories.
const (
	// Configuration errors
	ErrConfigInvalid   Code = "CONFIG_INVALID"
	ErrConfigNotFound  Code = "CONFIG_NOT_FOUND"
	ErrConfigParseFail Code = "CONFIG_PARSE_FAILED"

	// Cache errors
	ErrCacheInit      Code = "CACHE_INIT_FAILED"
	ErrCacheRead      Code = "CACHE_READ_FAILED"
	ErrCacheWrite     Code = "CACHE_WRITE_FAILED"
	ErrCachePurge     Code = "CACHE_PURGE_FAILED"
	ErrCacheCleanup   Code = "CACHE_CLEANUP_FAILED"
	ErrCacheDirAccess Code = "CACHE_DIR_ACCESS_FAILED"

	// Mirror errors
	ErrMirrorTimeout     Code = "MIRROR_TIMEOUT"
	ErrMirrorUnreachable Code = "MIRROR_UNREACHABLE"
	ErrMirrorBenchmark   Code = "MIRROR_BENCHMARK_FAILED"
	ErrMirrorInvalid     Code = "MIRROR_INVALID"

	// Provider/upstream errors
	ErrProviderDown      Code = "PROVIDER_DOWN"
	ErrProviderTimeout   Code = "PROVIDER_TIMEOUT"
	ErrUpstreamError     Code = "UPSTREAM_ERROR"
	ErrUpstreamNotFound  Code = "UPSTREAM_NOT_FOUND"
	ErrUpstreamForbidden Code = "UPSTREAM_FORBIDDEN"

	// Server errors
	ErrServerInit     Code = "SERVER_INIT_FAILED"
	ErrServerShutdown Code = "SERVER_SHUTDOWN_FAILED"
	ErrServerStart    Code = "SERVER_START_FAILED"

	// Authentication errors
	ErrAuthRequired     Code = "AUTH_REQUIRED"
	ErrAuthInvalid      Code = "AUTH_INVALID"
	ErrAuthExpired      Code = "AUTH_EXPIRED"
	ErrAuthInsufficient Code = "AUTH_INSUFFICIENT"

	// Request errors
	ErrRequestInvalid   Code = "REQUEST_INVALID"
	ErrRequestTimeout   Code = "REQUEST_TIMEOUT"
	ErrMethodNotAllowed Code = "METHOD_NOT_ALLOWED"
	ErrResourceNotFound Code = "RESOURCE_NOT_FOUND"
	ErrRateLimited      Code = "RATE_LIMITED"

	// Internal errors
	ErrInternal       Code = "INTERNAL_ERROR"
	ErrUnknown        Code = "UNKNOWN_ERROR"
	ErrNotImplemented Code = "NOT_IMPLEMENTED"
)

// AppError represents a structured application error with code, message, and optional cause.
type AppError struct {
	// Code is the unique error code for this error type.
	Code Code `json:"code"`

	// Message is a human-readable error message.
	Message string `json:"message"`

	// Details contains optional additional error details.
	Details map[string]interface{} `json:"details,omitempty"`

	// HTTPStatus is the suggested HTTP status code for this error.
	HTTPStatus int `json:"-"`

	// Cause is the underlying error that caused this error.
	Cause error `json:"-"`
}

// Error implements the error interface.
func (e *AppError) Error() string {
	if e.Cause != nil {
		return fmt.Sprintf("[%s] %s: %v", e.Code, e.Message, e.Cause)
	}
	return fmt.Sprintf("[%s] %s", e.Code, e.Message)
}

// Unwrap returns the underlying error for errors.Is and errors.As support.
func (e *AppError) Unwrap() error {
	return e.Cause
}

// WithCause sets the underlying cause of this error and returns the error.
func (e *AppError) WithCause(cause error) *AppError {
	e.Cause = cause
	return e
}

// WithDetails adds additional details to the error and returns the error.
func (e *AppError) WithDetails(key string, value interface{}) *AppError {
	if e.Details == nil {
		e.Details = make(map[string]interface{})
	}
	e.Details[key] = value
	return e
}

// WithHTTPStatus sets the suggested HTTP status code and returns the error.
func (e *AppError) WithHTTPStatus(status int) *AppError {
	e.HTTPStatus = status
	return e
}

// ToJSON returns the error as a JSON byte slice.
func (e *AppError) ToJSON() ([]byte, error) {
	return json.Marshal(e)
}

// New creates a new AppError with the given code and message.
func New(code Code, message string) *AppError {
	return &AppError{
		Code:       code,
		Message:    message,
		HTTPStatus: codeToHTTPStatus(code),
	}
}

// Wrap wraps an existing error with a new AppError.
func Wrap(code Code, message string, cause error) *AppError {
	return &AppError{
		Code:       code,
		Message:    message,
		Cause:      cause,
		HTTPStatus: codeToHTTPStatus(code),
	}
}

// codeToHTTPStatus maps error codes to suggested HTTP status codes.
func codeToHTTPStatus(code Code) int {
	switch code {
	// 400 Bad Request
	case ErrConfigInvalid, ErrRequestInvalid, ErrMirrorInvalid:
		return http.StatusBadRequest

	// 401 Unauthorized
	case ErrAuthRequired, ErrAuthInvalid, ErrAuthExpired:
		return http.StatusUnauthorized

	// 403 Forbidden
	case ErrAuthInsufficient, ErrUpstreamForbidden:
		return http.StatusForbidden

	// 404 Not Found
	case ErrResourceNotFound, ErrUpstreamNotFound, ErrConfigNotFound:
		return http.StatusNotFound

	// 405 Method Not Allowed
	case ErrMethodNotAllowed:
		return http.StatusMethodNotAllowed

	// 408 Request Timeout
	case ErrRequestTimeout, ErrMirrorTimeout, ErrProviderTimeout:
		return http.StatusRequestTimeout

	// 429 Too Many Requests
	case ErrRateLimited:
		return http.StatusTooManyRequests

	// 501 Not Implemented
	case ErrNotImplemented:
		return http.StatusNotImplemented

	// 502 Bad Gateway
	case ErrProviderDown, ErrMirrorUnreachable, ErrUpstreamError:
		return http.StatusBadGateway

	// 503 Service Unavailable
	case ErrServerInit, ErrServerStart:
		return http.StatusServiceUnavailable

	// 500 Internal Server Error (default)
	default:
		return http.StatusInternalServerError
	}
}

// HTTPErrorResponse represents an error response in HTTP API.
type HTTPErrorResponse struct {
	Error   string                 `json:"error"`
	Code    Code                   `json:"code,omitempty"`
	Message string                 `json:"message,omitempty"`
	Details map[string]interface{} `json:"details,omitempty"`
}

// WriteHTTPError writes an AppError as an HTTP JSON response.
func WriteHTTPError(w http.ResponseWriter, err *AppError) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(err.HTTPStatus)

	resp := HTTPErrorResponse{
		Error:   err.Message,
		Code:    err.Code,
		Details: err.Details,
	}

	_ = json.NewEncoder(w).Encode(resp)
}

// FromHTTPStatus creates an AppError from an HTTP status code.
func FromHTTPStatus(status int, message string) *AppError {
	code := ErrInternal
	switch status {
	case http.StatusBadRequest:
		code = ErrRequestInvalid
	case http.StatusUnauthorized:
		code = ErrAuthRequired
	case http.StatusForbidden:
		code = ErrAuthInsufficient
	case http.StatusNotFound:
		code = ErrResourceNotFound
	case http.StatusMethodNotAllowed:
		code = ErrMethodNotAllowed
	case http.StatusRequestTimeout:
		code = ErrRequestTimeout
	case http.StatusTooManyRequests:
		code = ErrRateLimited
	case http.StatusBadGateway:
		code = ErrUpstreamError
	case http.StatusServiceUnavailable:
		code = ErrProviderDown
	}

	return &AppError{
		Code:       code,
		Message:    message,
		HTTPStatus: status,
	}
}

// Is checks if the error is an AppError with the specified code.
func Is(err error, code Code) bool {
	if appErr, ok := err.(*AppError); ok {
		return appErr.Code == code
	}
	return false
}

// GetCode returns the error code if err is an AppError, otherwise returns ErrUnknown.
func GetCode(err error) Code {
	if appErr, ok := err.(*AppError); ok {
		return appErr.Code
	}
	return ErrUnknown
}

// GetHTTPStatus returns the HTTP status code for an error.
// If err is not an AppError, returns 500 Internal Server Error.
func GetHTTPStatus(err error) int {
	if appErr, ok := err.(*AppError); ok {
		return appErr.HTTPStatus
	}
	return http.StatusInternalServerError
}

// Common error constructors for frequently used errors

// ConfigError creates a configuration error.
func ConfigError(message string, cause error) *AppError {
	return Wrap(ErrConfigInvalid, message, cause)
}

// CacheError creates a cache-related error.
func CacheError(code Code, message string, cause error) *AppError {
	return Wrap(code, message, cause)
}

// MirrorError creates a mirror-related error.
func MirrorError(code Code, message string, cause error) *AppError {
	return Wrap(code, message, cause)
}

// AuthError creates an authentication error.
func AuthError(code Code, message string) *AppError {
	return New(code, message)
}

// ServerError creates a server-related error.
func ServerError(code Code, message string, cause error) *AppError {
	return Wrap(code, message, cause)
}

// InternalError creates an internal server error.
func InternalError(message string, cause error) *AppError {
	return Wrap(ErrInternal, message, cause)
}
