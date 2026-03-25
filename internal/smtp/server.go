package smtp

import (
	"context"
	"crypto/tls"
	"net"

	"go.uber.org/zap"

	"github.com/lyson-nexonode/gomail-core/config"
	"github.com/lyson-nexonode/gomail-core/internal/ports"
)

// Server listens for incoming SMTP connections on two ports:
// - plain port (2525 in dev, 587 in prod) with optional STARTTLS upgrade
// - TLS port (4650 in dev, 465 in prod) with implicit TLS
type Server struct {
	cfg         *config.Config
	log         *zap.Logger
	listener    net.Listener
	listenerTLS net.Listener
	delivery    ports.DeliveryPipeline
	tlsCfg      *tls.Config
}

// NewServer creates a new SMTP server.
func NewServer(cfg *config.Config, log *zap.Logger, delivery ports.DeliveryPipeline, tlsCfg *tls.Config) *Server {
	return &Server{
		cfg:      cfg,
		log:      log,
		delivery: delivery,
		tlsCfg:   tlsCfg,
	}
}

// Start begins listening for TCP connections on both plain and TLS ports.
func (s *Server) Start(ctx context.Context) error {
	var err error

	// Plain port — supports STARTTLS upgrade
	s.listener, err = net.Listen("tcp", s.cfg.SMTP.Addr)
	if err != nil {
		return err
	}

	s.log.Info("smtp server listening",
		zap.String("addr", s.cfg.SMTP.Addr),
		zap.String("domain", s.cfg.SMTP.Domain),
	)

	// TLS port — implicit TLS, every connection is encrypted from the first byte
	if s.tlsCfg != nil {
		s.listenerTLS, err = tls.Listen("tcp", s.cfg.SMTP.AddrTLS, s.tlsCfg)
		if err != nil {
			return err
		}
		s.log.Info("smtp tls server listening",
			zap.String("addr", s.cfg.SMTP.AddrTLS),
		)
		go s.acceptLoop(ctx, s.listenerTLS, true)
	}

	go func() {
		<-ctx.Done()
		s.log.Info("smtp server shutting down")
		_ = s.listener.Close()
		if s.listenerTLS != nil {
			_ = s.listenerTLS.Close()
		}
	}()

	// Plain listener runs in the main goroutine
	s.acceptLoop(ctx, s.listener, false)
	return nil
}

// acceptLoop accepts connections from a listener and spawns one session per connection.
// isTLS indicates whether the connection is already wrapped in TLS (implicit TLS port).
func (s *Server) acceptLoop(ctx context.Context, ln net.Listener, isTLS bool) {
	for {
		conn, err := ln.Accept()
		if err != nil {
			select {
			case <-ctx.Done():
				return
			default:
				s.log.Error("smtp accept error", zap.Error(err))
				continue
			}
		}

		s.log.Info("smtp connection accepted",
			zap.String("remote", conn.RemoteAddr().String()),
			zap.Bool("tls", isTLS),
		)

		go NewSession(conn, s.cfg, s.log, s.delivery, s.tlsCfg).Handle()
	}
}
