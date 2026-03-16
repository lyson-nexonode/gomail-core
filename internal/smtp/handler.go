package smtp

import (
	"bufio"
	"context"
	"fmt"
	"strings"
	"time"

	"go.uber.org/zap"

	"github.com/lyson-nexonode/gomail-core/internal/ports"
)

// handleEHLO processes EHLO and HELO commands (RFC 5321 section 4.1.1.1).
func (s *Session) handleEHLO(args string) {
	if args == "" {
		s.write("501 Syntax: EHLO hostname")
		return
	}

	if !s.transition(EventEHLO) {
		return
	}

	s.envelope.Reset()

	s.write(fmt.Sprintf("250-%s greets %s", s.cfg.SMTP.Domain, args))
	s.write(fmt.Sprintf("250-SIZE %d", s.cfg.SMTP.MaxSize))
	s.write("250-8BITMIME")
	s.write("250 ENHANCEDSTATUSCODES")
}

// handleMAIL processes the MAIL FROM command (RFC 5321 section 4.1.1.2).
func (s *Session) handleMAIL(args string) {
	addr, ok := extractAddress(args, "FROM:")
	if !ok {
		s.write("501 Syntax: MAIL FROM:<address>")
		return
	}

	if !s.transition(EventMailFrom) {
		return
	}

	s.envelope.From = addr
	s.write("250 OK")
	s.log.Info("smtp mail from accepted",
		zap.String("session", s.id),
		zap.String("from", addr),
	)
}

// handleRCPT processes the RCPT TO command (RFC 5321 section 4.1.1.3).
func (s *Session) handleRCPT(args string) {
	addr, ok := extractAddress(args, "TO:")
	if !ok {
		s.write("501 Syntax: RCPT TO:<address>")
		return
	}

	if len(s.envelope.To) >= 100 {
		s.write("452 Too many recipients")
		return
	}

	if !s.transition(EventRcptTo) {
		return
	}

	s.envelope.AddRecipient(addr)
	s.write("250 OK")
	s.log.Info("smtp rcpt to accepted",
		zap.String("session", s.id),
		zap.String("to", addr),
	)
}

// handleDATA processes the DATA command (RFC 5321 section 4.1.1.4).
// Once the message body is fully received, it publishes a MessageReceived event
// to the delivery pipeline — the SMTP session knows nothing about storage.
func (s *Session) handleDATA() {
	if !s.transition(EventData) {
		return
	}

	s.write("354 Start mail input; end with <CRLF>.<CRLF>")

	var body strings.Builder
	reader := bufio.NewReader(s.conn)

	for {
		s.conn.SetReadDeadline(time.Now().Add(10 * time.Minute))

		line, err := reader.ReadString('\n')
		if err != nil {
			s.log.Error("smtp data read error",
				zap.String("session", s.id),
				zap.Error(err),
			)
			return
		}

		// End-of-data marker (RFC 5321 section 4.5.2)
		if line == ".\r\n" || line == ".\n" {
			break
		}

		// Dot-unstuffing: remove the leading dot from dot-stuffed lines
		if strings.HasPrefix(line, "..") {
			line = line[1:]
		}

		body.WriteString(line)

		if int64(body.Len()) > s.cfg.SMTP.MaxSize {
			s.write("552 Message size exceeds maximum permitted")
			s.envelope.Reset()
			s.transition(EventRset)
			return
		}
	}

	s.envelope.Data = []byte(body.String())
	s.envelope.Size = int64(len(s.envelope.Data))
	s.envelope.ReceivedAt = time.Now()

	// Publish a MessageReceived event to the delivery pipeline.
	// The SMTP session is decoupled from storage — it only knows the port.
	event := ports.MessageReceived{
		From:      s.envelope.From,
		To:        s.envelope.To,
		Body:      s.envelope.Data,
		Size:      s.envelope.Size,
		Timestamp: s.envelope.ReceivedAt,
	}

	ctx := context.Background()
	if err := s.delivery.Deliver(ctx, event); err != nil {
		s.log.Error("delivery pipeline failed",
			zap.String("session", s.id),
			zap.Error(err),
		)
		s.write("451 Requested action aborted: error in processing")
		return
	}

	s.transition(EventDone)
	s.write("250 OK: message accepted for delivery")
}

// handleRSET processes the RSET command (RFC 5321 section 4.1.1.5).
func (s *Session) handleRSET() {
	if !s.transition(EventRset) {
		return
	}
	s.envelope.Reset()
	s.write("250 OK")
}

// handleQUIT processes the QUIT command (RFC 5321 section 4.1.1.10).
func (s *Session) handleQUIT() {
	s.transition(EventQuit)
	s.write(fmt.Sprintf("221 %s Service closing transmission channel", s.cfg.SMTP.Domain))
	s.log.Info("smtp session quit", zap.String("session", s.id))
}

// extractAddress parses an address from MAIL FROM or RCPT TO arguments.
func extractAddress(args, prefix string) (string, bool) {
	upper := strings.ToUpper(args)
	if !strings.HasPrefix(upper, prefix) {
		return "", false
	}

	rest := args[len(prefix):]
	rest = strings.TrimSpace(rest)

	if strings.HasPrefix(rest, "<") && strings.HasSuffix(rest, ">") {
		return rest[1 : len(rest)-1], true
	}

	if rest != "" {
		return rest, true
	}

	return "", false
}
