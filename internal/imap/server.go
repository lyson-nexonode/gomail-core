package imap

import (
	"context"
	"crypto/tls"
	"net"

	"go.uber.org/zap"

	"github.com/lyson-nexonode/gomail-core/config"
	"github.com/lyson-nexonode/gomail-core/internal/ports"
)

// Server listens for incoming IMAP connections on two ports:
// - plain port (1430 in dev, 143 in prod) with optional STARTTLS upgrade
// - TLS port (9930 in dev, 993 in prod) with implicit TLS
type Server struct {
	cfg            *config.Config
	log            *zap.Logger
	listener       net.Listener
	listenerTLS    net.Listener
	mailboxReader  ports.MailboxReader
	messageReader  ports.MessageReader
	domainResolver ports.DomainResolver
	userAuth       ports.UserAuthenticator
	tlsCfg         *tls.Config
}

// NewServer creates a new IMAP server with the given configuration and storage ports.
func NewServer(
	cfg *config.Config,
	log *zap.Logger,
	mailboxReader ports.MailboxReader,
	messageReader ports.MessageReader,
	domainResolver ports.DomainResolver,
	userAuth ports.UserAuthenticator,
	tlsCfg *tls.Config,
) *Server {
	return &Server{
		cfg:            cfg,
		log:            log,
		mailboxReader:  mailboxReader,
		messageReader:  messageReader,
		domainResolver: domainResolver,
		userAuth:       userAuth,
		tlsCfg:         tlsCfg,
	}
}

// Start begins listening for TCP connections on both plain and TLS ports.
func (s *Server) Start(ctx context.Context) error {
	var err error

	// Plain port — supports STARTTLS upgrade
	s.listener, err = net.Listen("tcp", s.cfg.IMAP.Addr)
	if err != nil {
		return err
	}

	s.log.Info("imap server listening", zap.String("addr", s.cfg.IMAP.Addr))

	// TLS port — implicit TLS, every connection is encrypted from the first byte
	if s.tlsCfg != nil {
		s.listenerTLS, err = tls.Listen("tcp", s.cfg.IMAP.AddrTLS, s.tlsCfg)
		if err != nil {
			return err
		}
		s.log.Info("imap tls server listening", zap.String("addr", s.cfg.IMAP.AddrTLS))
		go s.acceptLoop(ctx, s.listenerTLS, true)
	}

	go func() {
		<-ctx.Done()
		s.log.Info("imap server shutting down")
		_ = s.listener.Close()
		if s.listenerTLS != nil {
			_ = s.listenerTLS.Close()
		}
	}()

	s.acceptLoop(ctx, s.listener, false)
	return nil
}

// acceptLoop accepts connections and spawns one session per connection.
func (s *Server) acceptLoop(ctx context.Context, ln net.Listener, isTLS bool) {
	for {
		conn, err := ln.Accept()
		if err != nil {
			select {
			case <-ctx.Done():
				return
			default:
				s.log.Error("imap accept error", zap.Error(err))
				continue
			}
		}

		s.log.Info("imap connection accepted",
			zap.String("remote", conn.RemoteAddr().String()),
		)

		go newSession(
			conn, s.cfg, s.log,
			s.mailboxReader,
			s.messageReader,
			s.domainResolver,
			s.userAuth,
			defaultReadTimeout,
			s.tlsCfg,
			isTLS,
		).Handle()
	}
}
