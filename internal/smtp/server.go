package smtp

import (
	"context"
	"net"

	"go.uber.org/zap"

	"github.com/lyson-nexonode/gomail-core/config"
)

// Server listens for incoming SMTP connections and spawns one Session per client.
// Each session runs in its own goroutine, enabling high concurrency.
type Server struct {
	cfg      *config.Config
	log      *zap.Logger
	listener net.Listener
}

// NewServer creates a new SMTP server with the given configuration.
func NewServer(cfg *config.Config, log *zap.Logger) *Server {
	return &Server{
		cfg: cfg,
		log: log,
	}
}

// Start begins listening for TCP connections on the configured address.
// It blocks until the context is cancelled or a fatal error occurs.
func (s *Server) Start(ctx context.Context) error {
	var err error
	s.listener, err = net.Listen("tcp", s.cfg.SMTP.Addr)
	if err != nil {
		return err
	}
	defer s.listener.Close()

	s.log.Info("smtp server listening",
		zap.String("addr", s.cfg.SMTP.Addr),
		zap.String("domain", s.cfg.SMTP.Domain),
	)

	// Watch for context cancellation to shut down gracefully
	go func() {
		<-ctx.Done()
		s.log.Info("smtp server shutting down")
		s.listener.Close()
	}()

	for {
		conn, err := s.listener.Accept()
		if err != nil {
			select {
			case <-ctx.Done():
				// Expected shutdown, not an error
				return nil
			default:
				s.log.Error("smtp accept error", zap.Error(err))
				continue
			}
		}

		s.log.Info("smtp connection accepted",
			zap.String("remote", conn.RemoteAddr().String()),
		)

		// Each client runs in its own goroutine
		// This is the Go concurrency model: one goroutine per connection
		go NewSession(conn, s.cfg, s.log).Handle()
	}
}
