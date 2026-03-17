package config

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestGetEnv verifies that getEnv returns the env variable value
// when set, and the fallback when not set.
func TestGetEnv(t *testing.T) {
	t.Run("returns env variable when set", func(t *testing.T) {
		os.Setenv("TEST_KEY", "testvalue")
		defer os.Unsetenv("TEST_KEY")
		assert.Equal(t, "testvalue", getEnv("TEST_KEY", "fallback"))
	})

	t.Run("returns fallback when not set", func(t *testing.T) {
		os.Unsetenv("TEST_KEY")
		assert.Equal(t, "fallback", getEnv("TEST_KEY", "fallback"))
	})

	t.Run("returns fallback when empty", func(t *testing.T) {
		os.Setenv("TEST_KEY", "")
		defer os.Unsetenv("TEST_KEY")
		assert.Equal(t, "fallback", getEnv("TEST_KEY", "fallback"))
	})
}

// TestGetEnvInt verifies integer parsing from environment variables.
func TestGetEnvInt(t *testing.T) {
	t.Run("returns int value when set", func(t *testing.T) {
		os.Setenv("TEST_INT", "42")
		defer os.Unsetenv("TEST_INT")
		assert.Equal(t, 42, getEnvInt("TEST_INT", 0))
	})

	t.Run("returns fallback when not set", func(t *testing.T) {
		os.Unsetenv("TEST_INT")
		assert.Equal(t, 10, getEnvInt("TEST_INT", 10))
	})

	t.Run("returns fallback when invalid", func(t *testing.T) {
		os.Setenv("TEST_INT", "notanint")
		defer os.Unsetenv("TEST_INT")
		assert.Equal(t, 5, getEnvInt("TEST_INT", 5))
	})
}

// TestLoad verifies that Load returns a config with default values.
func TestLoad(t *testing.T) {
	cfg := Load()

	assert.NotNil(t, cfg)
	assert.Equal(t, "development", cfg.Env)
	assert.NotEmpty(t, cfg.MySQL.DSN)
	assert.NotEmpty(t, cfg.Redis.Addr)
	assert.NotEmpty(t, cfg.SMTP.Addr)
	assert.NotEmpty(t, cfg.IMAP.Addr)
	assert.NotEmpty(t, cfg.JMAP.Addr)
	assert.Greater(t, cfg.SMTP.MaxSize, int64(0))
}

// TestLoadWithEnv verifies that Load picks up environment variables.
func TestLoadWithEnv(t *testing.T) {
	os.Setenv("GOMAIL_ENV", "production")
	os.Setenv("GOMAIL_SMTP_DOMAIN", "test.local")
	defer func() {
		os.Unsetenv("GOMAIL_ENV")
		os.Unsetenv("GOMAIL_SMTP_DOMAIN")
	}()

	cfg := Load()
	assert.Equal(t, "production", cfg.Env)
	assert.Equal(t, "test.local", cfg.SMTP.Domain)
}
