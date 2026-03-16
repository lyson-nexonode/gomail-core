package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"go.uber.org/zap"

	"github.com/lyson-nexonode/gomail-core/config"
	"github.com/lyson-nexonode/gomail-core/internal/imap"
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
	defer log.Sync()

	log.Info("gomail-core imap starting",
		zap.String("env", cfg.Env),
		zap.String("addr", cfg.IMAP.Addr),
	)

	// Initialize MySQL store
	mysql, err := mysqlstore.New(cfg.MySQL.DSN, log)
	if err != nil {
		log.Fatal("mysql init failed", zap.Error(err))
	}
	defer mysql.Close()

	// Initialize Redis store
	rdb, err := redisstore.New(cfg.Redis.Addr, cfg.Redis.Password, cfg.Redis.DB, log)
	if err != nil {
		log.Fatal("redis init failed", zap.Error(err))
	}
	defer rdb.Close()

	// Wire the message store — implements all required ports
	ms := storage.NewMessageStore(mysql, rdb, log)

	// Start pprof on a separate goroutine, never on a public port
	telemetry.StartPPROF("localhost:6062", log)

	// Graceful shutdown on SIGINT or SIGTERM
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	server := imap.NewServer(cfg, log, ms, ms, ms, ms)
	if err := server.Start(ctx); err != nil {
		log.Fatal("imap server failed", zap.Error(err))
	}

	log.Info("imap server stopped")
}
