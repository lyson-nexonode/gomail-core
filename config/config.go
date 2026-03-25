package config

import (
	"log"
	"os"
	"strconv"
)

// Config holds all configuration for gomail-core services.
// Values are loaded from environment variables with sensible defaults.
type Config struct {
	Env       string
	MySQL     MySQLConfig
	Redis     RedisConfig
	SMTP      SMTPConfig
	IMAP      IMAPConfig
	JMAP      JMAPConfig
	TLS       TLSConfig
	Telemetry TelemetryConfig
}

// MySQLConfig holds the connection parameters for the MySQL database.
type MySQLConfig struct {
	DSN string
}

// RedisConfig holds the connection parameters for the Redis instance.
type RedisConfig struct {
	Addr     string
	Password string
	DB       int
}

// SMTPConfig holds the network and protocol settings for the SMTP server.
type SMTPConfig struct {
	Addr        string
	AddrTLS     string // port 465 — implicit TLS
	Domain      string
	MaxSize     int64
}

// IMAPConfig holds the network settings for the IMAP server.
type IMAPConfig struct {
	Addr    string // port 143 — STARTTLS
	AddrTLS string // port 993 — implicit TLS
}

// JMAPConfig holds the network settings for the JMAP server.
type JMAPConfig struct {
	Addr string
}

// TLSConfig holds the paths to the TLS certificate and key files.
type TLSConfig struct {
	CertFile string
	KeyFile  string
	Enabled  bool
}

// TelemetryConfig holds the settings for observability endpoints.
type TelemetryConfig struct {
	PPROFAddr string
}

// Load reads configuration from environment variables.
// Falls back to development defaults when variables are not set.
func Load() *Config {
	return &Config{
		Env: getEnv("GOMAIL_ENV", "development"),
		MySQL: MySQLConfig{
			DSN: getEnv("GOMAIL_MYSQL_DSN", "gomail:gomailpassword@tcp(localhost:3306)/gomail?parseTime=true"),
		},
		Redis: RedisConfig{
			Addr:     getEnv("GOMAIL_REDIS_ADDR", "localhost:6379"),
			Password: getEnv("GOMAIL_REDIS_PASSWORD", ""),
			DB:       getEnvInt("GOMAIL_REDIS_DB", 0),
		},
		SMTP: SMTPConfig{
			Addr:    getEnv("GOMAIL_SMTP_ADDR", ":2525"),
			AddrTLS: getEnv("GOMAIL_SMTP_ADDR_TLS", ":4650"),
			Domain:  getEnv("GOMAIL_SMTP_DOMAIN", "gomail.local"),
			MaxSize: int64(getEnvInt("GOMAIL_SMTP_MAX_SIZE", 26214400)),
		},
		IMAP: IMAPConfig{
			Addr:    getEnv("GOMAIL_IMAP_ADDR", ":1430"),
			AddrTLS: getEnv("GOMAIL_IMAP_ADDR_TLS", ":9930"),
		},
		JMAP: JMAPConfig{
			Addr: getEnv("GOMAIL_JMAP_ADDR", ":8080"),
		},
		TLS: TLSConfig{
			CertFile: getEnv("GOMAIL_TLS_CERT", "certs/server.crt"),
			KeyFile:  getEnv("GOMAIL_TLS_KEY", "certs/server.key"),
			Enabled:  getEnvBool("GOMAIL_TLS_ENABLED", true),
		},
		Telemetry: TelemetryConfig{
			PPROFAddr: getEnv("GOMAIL_PPROF_ADDR", "localhost:6061"),
		},
	}
}

// getEnv returns the value of an environment variable or a fallback default.
func getEnv(key, fallback string) string {
	if val := os.Getenv(key); val != "" {
		return val
	}
	return fallback
}

// getEnvInt returns the integer value of an environment variable or a fallback default.
func getEnvInt(key string, fallback int) int {
	val := os.Getenv(key)
	if val == "" {
		return fallback
	}
	n, err := strconv.Atoi(val)
	if err != nil {
		log.Printf("invalid value for %s: %s, using default %d", key, val, fallback)
		return fallback
	}
	return n
}

// getEnvBool returns the boolean value of an environment variable or a fallback default.
func getEnvBool(key string, fallback bool) bool {
	val := os.Getenv(key)
	if val == "" {
		return fallback
	}
	b, err := strconv.ParseBool(val)
	if err != nil {
		log.Printf("invalid value for %s: %s, using default %v", key, val, fallback)
		return fallback
	}
	return b
}
