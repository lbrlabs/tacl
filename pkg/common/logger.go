package common

import (
	"go.uber.org/zap"
)

// InitializeLogger creates a Zap logger (production config).
func InitializeLogger() *zap.Logger {
	logger, err := zap.NewProduction()
	if err != nil {
		panic("Failed to initialize logger")
	}
	return logger
}
