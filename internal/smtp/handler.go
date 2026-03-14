package smtp

import (
	"bufio"
	"fmt"
	"strings"
	"time"

	"go.uber.org/zap"
)

// handleEHLO processes the EHLO and HELO commands (RFC 5321 section 4.1.1.1).
// EHLO is the modern form and advertises server capabilities.
// On success, the FSM transitions to StateGreeted.
func (s *Session) handleEHLO(args string) {
	if args == "" {
		s.write("501 Syntax: EHLO hostname")
		return
	}

	if !s.transition(EventEHLO) {
		return
	}

	// Reset any in-progress transaction when client re-identifies
	s.envelope.Reset()

	// Advertise server capabilities to the client
	s.write(fmt.Sprintf("250-%s greets %s", s.cfg.SMTP.Domain, args))
	s.write(fmt.Sprintf("250-SIZE %d", s.cfg.SMTP.MaxSize))
	s.write("250-8BITMIME")
	s.write("250 ENHANCEDSTATUSCODES")
}

// handleMAIL processes the MAIL FROM command (RFC 5321 section 4.1.1.2).
// Extracts the sender address and records it in the envelope.
// On success, the FSM transitions to StateMailFrom.
func (s *Session) handleMAIL(args string) {
	// args is expected to be: "FROM:<alice@example.com>"
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
// Validates that the recipient domain is handled by this server.
// On success, the FSM transitions to StateRcptTo (or stays there for multiple recipients).
func (s *Session) handleRCPT(args string) {
	addr, ok := extractAddress(args, "TO:")
	if !ok {
		s.write("501 Syntax: RCPT TO:<address>")
		return
	}

	// Basic recipient limit to prevent abuse
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
// Reads the message body until the end-of-data sequence (CRLF.CRLF).
// Implements dot-stuffing: a line starting with "." is either the end marker
// or a literal dot (the leading dot is stripped per RFC 5321 section 4.5.2).
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

		// End-of-data marker: a line containing only a dot (RFC 5321 section 4.5.2)
		if line == ".\r\n" || line == ".\n" {
			break
		}

		// Dot-unstuffing: remove the leading dot from dot-stuffed lines
		if strings.HasPrefix(line, "..") {
			line = line[1:]
		}

		body.WriteString(line)

		// Enforce maximum message size
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

	// TODO: persist the message to storage (MySQL + Redis) — Day 3
	s.log.Info("smtp message received",
		zap.String("session", s.id),
		zap.String("from", s.envelope.From),
		zap.Strings("to", s.envelope.To),
		zap.Int64("size", s.envelope.Size),
	)

	// Transition back to greeted so the client can send another message
	s.transition(EventDone)
	s.write("250 OK: message accepted for delivery")
}

// handleRSET processes the RSET command (RFC 5321 section 4.1.1.5).
// Resets the current mail transaction without closing the session.
func (s *Session) handleRSET() {
	if !s.transition(EventRset) {
		return
	}
	s.envelope.Reset()
	s.write("250 OK")
}

// handleQUIT processes the QUIT command (RFC 5321 section 4.1.1.10).
// Sends the goodbye response and lets Handle() close the connection.
func (s *Session) handleQUIT() {
	s.transition(EventQuit)
	s.write(fmt.Sprintf("221 %s Service closing transmission channel", s.cfg.SMTP.Domain))
	s.log.Info("smtp session quit", zap.String("session", s.id))
}

// extractAddress parses an address from a MAIL FROM or RCPT TO argument.
// Input examples: "FROM:<alice@example.com>" or "TO:<bob@example.com>"
// Returns the address without angle brackets and true on success.
func extractAddress(args, prefix string) (string, bool) {
	upper := strings.ToUpper(args)
	if !strings.HasPrefix(upper, prefix) {
		return "", false
	}

	rest := args[len(prefix):]
	rest = strings.TrimSpace(rest)

	// Strip angle brackets
	if strings.HasPrefix(rest, "<") && strings.HasSuffix(rest, ">") {
		return rest[1 : len(rest)-1], true
	}

	// Accept addresses without angle brackets (some clients omit them)
	if rest != "" {
		return rest, true
	}

	return "", false
}
