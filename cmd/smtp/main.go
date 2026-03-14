package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"go.uber.org/zap"

	"github.com/lyson-nexonode/gomail-core/config"
	"github.com/lyson-nexonode/gomail-core/internal/smtp"
	"github.com/lyson-nexonode/gomail-core/internal/telemetry"
)

func main() {
	cfg := config.Load()

	log, err := telemetry.NewLogger(cfg.Env)
	if err != nil {
		panic("failed to initialize logger: " + err.Error())
	}
	defer log.Sync()

	log.Info("gomail-core smtp starting",
		zap.String("env", cfg.Env),
		zap.String("addr", cfg.SMTP.Addr),
		zap.String("domain", cfg.SMTP.Domain),
	)

	// Start pprof on a separate goroutine, never on a public port
	telemetry.StartPPROF(cfg.Telemetry.PPROFAddr, log)

	// Graceful shutdown on SIGINT or SIGTERM
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	server := smtp.NewServer(cfg, log)
	if err := server.Start(ctx); err != nil {
		log.Fatal("smtp server failed", zap.Error(err))
	}

	log.Info("smtp server stopped")
}
