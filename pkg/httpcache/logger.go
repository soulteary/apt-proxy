package httpcache

import (
	logger "github.com/soulteary/logger-kit"
)

// DebugLogging controls whether debug messages are logged
var DebugLogging = false

// cacheLogger is the logger instance used by httpcache package
var cacheLogger = logger.Default()

// SetLogger sets the logger instance for the httpcache package
func SetLogger(log *logger.Logger) {
	if log != nil {
		cacheLogger = log
	}
}

func debugf(format string, args ...interface{}) {
	if DebugLogging {
		cacheLogger.Debug().Msgf(format, args...)
	}
}

func errorf(format string, args ...interface{}) {
	cacheLogger.Error().Msgf(format, args...)
}
