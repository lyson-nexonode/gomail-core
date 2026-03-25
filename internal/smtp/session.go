package smtp

import (
	"bufio"
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"strings"
	"time"

	llfsm "github.com/looplab/fsm"
	"go.uber.org/zap"

	"github.com/lyson-nexonode/gomail-core/config"
	"github.com/lyson-nexonode/gomail-core/internal/ports"
)

// State represents a single SMTP session state.
// The FSM enforces valid command sequences as defined in RFC 5321.
type State string

const (
	StateInit     State = "init"
	StateGreeted  State = "greeted"
	StateMailFrom State = "mail_from"
	StateRcptTo   State = "rcpt_to"
	StateData     State = "data"
	StateQuit     State = "quit"
)

// SMTPEvent represents a FSM transition trigger.
type SMTPEvent string

const (
	EventEHLO     SMTPEvent = "ehlo"
	EventMailFrom SMTPEvent = "mail_from"
	EventRcptTo   SMTPEvent = "rcpt_to"
	EventData     SMTPEvent = "data"
	EventDone     SMTPEvent = "done"
	EventRset     SMTPEvent = "rset"
	EventQuit     SMTPEvent = "quit"
)

// Session represents a single client connection and its FSM state.
// It depends only on ports.DeliveryPipeline — never on concrete storage.
type Session struct {
	conn     net.Conn
	reader   *bufio.Reader
	fsm      *llfsm.FSM
	envelope *Envelope
	cfg      *config.Config
	log      *zap.Logger
	id       string
	delivery ports.DeliveryPipeline
	tlsCfg   *tls.Config
	isTLS    bool // true after STARTTLS upgrade or on implicit TLS port
}

// NewSession creates a new SMTP session for the given connection.
func NewSession(conn net.Conn, cfg *config.Config, log *zap.Logger, delivery ports.DeliveryPipeline, tlsCfg *tls.Config) *Session {
	s := &Session{
		conn:     conn,
		reader:   bufio.NewReader(conn),
		envelope: &Envelope{},
		cfg:      cfg,
		log:      log.With(zap.String("remote", conn.RemoteAddr().String())),
		id:       fmt.Sprintf("%d", time.Now().UnixNano()),
		delivery: delivery,
		tlsCfg:   tlsCfg,
	}

	// Detect if the connection is already TLS (implicit TLS port)
	if _, ok := conn.(*tls.Conn); ok {
		s.isTLS = true
	}

	s.fsm = newSMTPFSM()
	return s
}

// newSMTPFSM builds the SMTP session FSM.
func newSMTPFSM() *llfsm.FSM {
	return llfsm.NewFSM(
		string(StateInit),
		llfsm.Events{
			{Name: string(EventEHLO), Src: []string{string(StateInit), string(StateGreeted)}, Dst: string(StateGreeted)},
			{Name: string(EventMailFrom), Src: []string{string(StateGreeted)}, Dst: string(StateMailFrom)},
			{Name: string(EventRcptTo), Src: []string{string(StateMailFrom)}, Dst: string(StateRcptTo)},
			{Name: string(EventData), Src: []string{string(StateRcptTo)}, Dst: string(StateData)},
			{Name: string(EventDone), Src: []string{string(StateData)}, Dst: string(StateGreeted)},
			{Name: string(EventRset), Src: []string{
				string(StateGreeted),
				string(StateMailFrom),
				string(StateRcptTo),
				string(StateData),
			}, Dst: string(StateGreeted)},
			{Name: string(EventQuit), Src: []string{
				string(StateInit),
				string(StateGreeted),
				string(StateMailFrom),
				string(StateRcptTo),
				string(StateData),
			}, Dst: string(StateQuit)},
		},
		llfsm.Callbacks{},
	)
}

// Handle runs the session read loop until the client disconnects or sends QUIT.
func (s *Session) Handle() {
	defer func() { _ = s.conn.Close() }()

	s.log.Info("smtp session started", zap.String("session", s.id), zap.Bool("tls", s.isTLS))
	s.write(fmt.Sprintf("220 %s ESMTP gomail-core ready", s.cfg.SMTP.Domain))

	for {
		_ = s.conn.SetReadDeadline(time.Now().Add(5 * time.Minute))

		line, err := s.reader.ReadString('\n')
		if err != nil {
			s.log.Info("smtp session closed", zap.String("session", s.id), zap.Error(err))
			return
		}

		line = strings.TrimRight(line, "\r\n")
		if line == "" {
			continue
		}

		s.log.Debug("smtp received", zap.String("session", s.id), zap.String("line", line))

		cmd, args, _ := strings.Cut(line, " ")
		cmd = strings.ToUpper(strings.TrimSpace(cmd))

		switch cmd {
		case "EHLO", "HELO":
			s.handleEHLO(args)
		case "STARTTLS":
			s.handleSTARTTLS()
		case "MAIL":
			s.handleMAIL(args)
		case "RCPT":
			s.handleRCPT(args)
		case "DATA":
			s.handleDATA()
		case "RSET":
			s.handleRSET()
		case "QUIT":
			s.handleQUIT()
			return
		case "NOOP":
			s.write("250 OK")
		default:
			s.write("500 Command not recognized")
		}
	}
}

// handleSTARTTLS upgrades the plain connection to TLS (RFC 3207).
// After the upgrade, the session reader is reset to read from the TLS connection.
func (s *Session) handleSTARTTLS() {
	if s.isTLS {
		s.write("503 Already using TLS")
		return
	}

	if s.tlsCfg == nil {
		s.write("454 TLS not available")
		return
	}

	// Reject STARTTLS after MAIL FROM — the transaction must be restarted
	if s.currentState() == StateMailFrom || s.currentState() == StateRcptTo {
		s.write("503 Bad sequence of commands")
		return
	}

	s.write("220 Ready to start TLS")

	// Wrap the existing connection in TLS
	tlsConn := tls.Server(s.conn, s.tlsCfg)
	if err := tlsConn.Handshake(); err != nil {
		s.log.Error("smtp starttls handshake failed",
			zap.String("session", s.id),
			zap.Error(err),
		)
		return
	}

	// Replace connection and reader with the TLS-wrapped versions
	s.conn = tlsConn
	s.reader = bufio.NewReader(tlsConn)
	s.isTLS = true

	// Reset the FSM — client must re-issue EHLO after STARTTLS (RFC 3207 section 4.2)
	s.fsm = newSMTPFSM()
	s.envelope.Reset()

	s.log.Info("smtp starttls upgrade successful", zap.String("session", s.id))
}

// transition fires a FSM event and writes 503 if the transition is invalid.
func (s *Session) transition(event SMTPEvent) bool {
	if err := s.fsm.Event(context.Background(), string(event)); err != nil {
		s.log.Warn("smtp invalid transition",
			zap.String("session", s.id),
			zap.String("event", string(event)),
			zap.String("current_state", s.fsm.Current()),
		)
		s.write("503 Bad sequence of commands")
		return false
	}
	return true
}

// transitionRcptTo handles RCPT TO specifically.
// Additional RCPT TO commands are valid without a FSM transition.
func (s *Session) transitionRcptTo() bool {
	if s.currentState() == StateRcptTo {
		return true
	}
	return s.transition(EventRcptTo)
}

// write sends a response line to the client with CRLF as required by RFC 5321.
func (s *Session) write(line string) {
	_, _ = fmt.Fprintf(s.conn, "%s\r\n", line)
	s.log.Debug("smtp sent", zap.String("session", s.id), zap.String("line", line))
}

// currentState returns the current FSM state.
func (s *Session) currentState() State {
	return State(s.fsm.Current())
}
