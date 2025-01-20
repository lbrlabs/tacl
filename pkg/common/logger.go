package common

import (
	"io"

	"go.uber.org/zap"
)

// InitializeLogger creates a Zap logger at Info or Debug depending on 'debug'.
func InitializeLogger(debug bool) *zap.Logger {
	// Start with production config for JSON logs, etc.
	config := zap.NewProductionConfig()

	// If debug mode, set level to Debug instead of Info
	if debug {
		config.Level = zap.NewAtomicLevelAt(zap.DebugLevel)
	}

	logger, err := config.Build()
	if err != nil {
		panic("Failed to build zap logger: " + err.Error())
	}
	return logger
}

type zapWriter struct {
	logger *zap.Logger
}

func (zw *zapWriter) Write(p []byte) (n int, err error) {
	zw.logger.Debug(string(p))
	return len(p), nil
}

type noOpWriter struct{}

func (noOpWriter) Write(p []byte) (n int, err error) {
	return len(p), nil
}

// NewConditionalZapWriter returns an io.Writer that either discards everything
// (debug == false) or logs at Debug level (debug == true).
func NewConditionalZapWriter(debug bool, l *zap.Logger) io.Writer {
	if debug {
		return &zapWriter{logger: l}
	}
	return noOpWriter{}
}
