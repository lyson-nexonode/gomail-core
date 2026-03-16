package smtp

import (
	"bufio"
	"context"
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
}

// NewSession creates a new SMTP session for the given connection.
func NewSession(conn net.Conn, cfg *config.Config, log *zap.Logger, delivery ports.DeliveryPipeline) *Session {
	s := &Session{
		conn:     conn,
		reader:   bufio.NewReader(conn),
		envelope: &Envelope{},
		cfg:      cfg,
		log:      log.With(zap.String("remote", conn.RemoteAddr().String())),
		id:       fmt.Sprintf("%d", time.Now().UnixNano()),
		delivery: delivery,
	}

	s.fsm = newSMTPFSM()
	return s
}

// newSMTPFSM builds the SMTP session FSM.
// Extracted as a standalone function so it can be reused in tests.
func newSMTPFSM() *llfsm.FSM {
	return llfsm.NewFSM(
		string(StateInit),
		llfsm.Events{
			// EHLO/HELO can be sent from init or greeted (client re-identification)
			{Name: string(EventEHLO), Src: []string{string(StateInit), string(StateGreeted)}, Dst: string(StateGreeted)},

			// MAIL FROM is only valid after greeting
			{Name: string(EventMailFrom), Src: []string{string(StateGreeted)}, Dst: string(StateMailFrom)},

			// RCPT TO transitions from mail_from to rcpt_to only
			// Additional RCPT TO commands do not trigger FSM events — see handleRCPT
			{Name: string(EventRcptTo), Src: []string{string(StateMailFrom)}, Dst: string(StateRcptTo)},

			// DATA requires at least one recipient
			{Name: string(EventData), Src: []string{string(StateRcptTo)}, Dst: string(StateData)},

			// DONE is fired internally when the message body transfer completes
			{Name: string(EventDone), Src: []string{string(StateData)}, Dst: string(StateGreeted)},

			// RSET resets the transaction but keeps the session alive
			{Name: string(EventRset), Src: []string{
				string(StateGreeted),
				string(StateMailFrom),
				string(StateRcptTo),
				string(StateData),
			}, Dst: string(StateGreeted)},

			// QUIT is valid from any state
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

	s.log.Info("smtp session started", zap.String("session", s.id))
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
			// NOOP does nothing but must be acknowledged (RFC 5321 section 4.1.1.9)
			s.write("250 OK")
		default:
			s.write("500 Command not recognized")
		}
	}
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
// The first RCPT TO transitions from mail_from to rcpt_to.
// Subsequent RCPT TO commands are valid in rcpt_to without a FSM transition
// since looplab/fsm does not support self-transitions.
func (s *Session) transitionRcptTo() bool {
	if s.currentState() == StateRcptTo {
		// Already in rcpt_to — additional recipients are valid, no FSM event needed
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
