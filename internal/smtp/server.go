package smtp

import (
	"context"
	"net"

	"go.uber.org/zap"

	"github.com/lyson-nexonode/gomail-core/config"
	"github.com/lyson-nexonode/gomail-core/internal/ports"
)

// Server listens for incoming SMTP connections and spawns one Session per client.
// It depends only on the ports.DeliveryPipeline interface — never on concrete storage.
type Server struct {
	cfg      *config.Config
	log      *zap.Logger
	listener net.Listener
	delivery ports.DeliveryPipeline
}

// NewServer creates a new SMTP server.
// delivery is the port through which received messages are handed off for persistence.
func NewServer(cfg *config.Config, log *zap.Logger, delivery ports.DeliveryPipeline) *Server {
	return &Server{
		cfg:      cfg,
		log:      log,
		delivery: delivery,
	}
}

// Start begins listening for TCP connections on the configured address.
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
				return nil
			default:
				s.log.Error("smtp accept error", zap.Error(err))
				continue
			}
		}

		s.log.Info("smtp connection accepted",
			zap.String("remote", conn.RemoteAddr().String()),
		)

		go NewSession(conn, s.cfg, s.log, s.delivery).Handle()
	}
}
