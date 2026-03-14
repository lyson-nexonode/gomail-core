package telemetry

import (
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

// NewLogger creates a structured logger suited for the current environment.
// Development mode uses a human-readable colored format.
// Production mode uses JSON output for log aggregation pipelines.
func NewLogger(env string) (*zap.Logger, error) {
	if env == "development" {
		cfg := zap.NewDevelopmentConfig()
		cfg.EncoderConfig.EncodeLevel = zapcore.CapitalColorLevelEncoder
		return cfg.Build()
	}

	return zap.NewProduction()
}
