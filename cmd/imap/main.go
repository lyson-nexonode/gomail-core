package main

import (
	"context"
	"crypto/tls"
	"os"
	"os/signal"
	"syscall"

	"go.uber.org/zap"

	"github.com/lyson-nexonode/gomail-core/config"
	"github.com/lyson-nexonode/gomail-core/internal/imap"
	"github.com/lyson-nexonode/gomail-core/internal/security"
	"github.com/lyson-nexonode/gomail-core/internal/storage"
	mysqlstore "github.com/lyson-nexonode/gomail-core/internal/storage/mysql"
	redisstore "github.com/lyson-nexonode/gomail-core/internal/storage/redis"
	"github.com/lyson-nexonode/gomail-core/internal/telemetry"
)

func main() {
	cfg := config.Load()

	log, err := telemetry.NewLogger(cfg.Env)
	if err != nil {
		panic("failed to initialize logger: " + err.Error())
	}
	defer func() { _ = log.Sync() }()

	log.Info("gomail-core imap starting",
		zap.String("env", cfg.Env),
		zap.String("addr", cfg.IMAP.Addr),
	)

	mysql, err := mysqlstore.New(cfg.MySQL.DSN, log)
	if err != nil {
		log.Fatal("mysql init failed", zap.Error(err))
	}
	defer func() { _ = mysql.Close() }()

	rdb, err := redisstore.New(cfg.Redis.Addr, cfg.Redis.Password, cfg.Redis.DB, log)
	if err != nil {
		log.Fatal("redis init failed", zap.Error(err))
	}
	defer func() { _ = rdb.Close() }()

	ms := storage.NewMessageStore(mysql, rdb, log)

	// Load TLS config — nil disables TLS and implicit TLS listener
	var tlsCfg *tls.Config
	if cfg.TLS.Enabled {
		tlsCfg, err = security.LoadTLSConfig(cfg.TLS.CertFile, cfg.TLS.KeyFile)
		if err != nil {
			log.Warn("tls config failed — running without TLS", zap.Error(err))
		}
	}

	telemetry.StartPPROF("localhost:6062", log)

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	server := imap.NewServer(cfg, log, ms, ms, ms, ms, tlsCfg)
	if err := server.Start(ctx); err != nil {
		log.Fatal("imap server failed", zap.Error(err))
	}

	log.Info("imap server stopped")
}
