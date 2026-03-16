package imap

import (
	"context"
	"net"

	"go.uber.org/zap"

	"github.com/lyson-nexonode/gomail-core/config"
	"github.com/lyson-nexonode/gomail-core/internal/ports"
)

// Server listens for incoming IMAP connections and spawns one Session per client.
// It depends only on ports interfaces — never on concrete storage.
type Server struct {
	cfg            *config.Config
	log            *zap.Logger
	listener       net.Listener
	mailboxReader  ports.MailboxReader
	messageReader  ports.MessageReader
	domainResolver ports.DomainResolver
	userAuth       ports.UserAuthenticator
}

// NewServer creates a new IMAP server with the given configuration and storage ports.
func NewServer(
	cfg *config.Config,
	log *zap.Logger,
	mailboxReader ports.MailboxReader,
	messageReader ports.MessageReader,
	domainResolver ports.DomainResolver,
	userAuth ports.UserAuthenticator,
) *Server {
	return &Server{
		cfg:            cfg,
		log:            log,
		mailboxReader:  mailboxReader,
		messageReader:  messageReader,
		domainResolver: domainResolver,
		userAuth:       userAuth,
	}
}

// Start begins listening for TCP connections on the configured IMAP address.
func (s *Server) Start(ctx context.Context) error {
	var err error
	s.listener, err = net.Listen("tcp", s.cfg.IMAP.Addr)
	if err != nil {
		return err
	}
	defer func() { _ = s.listener.Close() }()

	s.log.Info("imap server listening", zap.String("addr", s.cfg.IMAP.Addr))

	go func() {
		<-ctx.Done()
		s.log.Info("imap server shutting down")
		_ = s.listener.Close()
	}()

	for {
		conn, err := s.listener.Accept()
		if err != nil {
			select {
			case <-ctx.Done():
				return nil
			default:
				s.log.Error("imap accept error", zap.Error(err))
				continue
			}
		}

		s.log.Info("imap connection accepted",
			zap.String("remote", conn.RemoteAddr().String()),
		)

		go NewSession(
			conn, s.cfg, s.log,
			s.mailboxReader,
			s.messageReader,
			s.domainResolver,
			s.userAuth,
		).Handle()
	}
}
