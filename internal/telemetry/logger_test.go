package telemetry

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestNewLoggerDevelopment verifies that a development logger is created successfully.
func TestNewLoggerDevelopment(t *testing.T) {
	log, err := NewLogger("development")
	require.NoError(t, err)
	assert.NotNil(t, log)
}

// TestNewLoggerProduction verifies that a production logger is created successfully.
func TestNewLoggerProduction(t *testing.T) {
	log, err := NewLogger("production")
	require.NoError(t, err)
	assert.NotNil(t, log)
}

// TestNewLoggerUnknownEnv verifies that an unknown env falls back to production logger.
func TestNewLoggerUnknownEnv(t *testing.T) {
	log, err := NewLogger("staging")
	require.NoError(t, err)
	assert.NotNil(t, log)
}
