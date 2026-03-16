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
	// StateInit is the initial state when a client connects.
	StateInit State = "init"

	// StateGreeted is entered after a valid EHLO or HELO command.
	StateGreeted State = "greeted"

	// StateMailFrom is entered after a valid MAIL FROM command.
	StateMailFrom State = "mail_from"

	// StateRcptTo is entered after at least one valid RCPT TO command.
	StateRcptTo State = "rcpt_to"

	// StateData is entered after the DATA command is accepted.
	StateData State = "data"

	// StateQuit is the terminal state after a QUIT command.
	StateQuit State = "quit"
)

// SMTPEvent represents a transition trigger in the SMTP FSM.
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
// delivery is the port through which received messages are handed off.
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

	s.fsm = llfsm.NewFSM(
		string(StateInit),
		llfsm.Events{
			// EHLO/HELO can be sent from init or greeted (client re-identification)
			{Name: string(EventEHLO), Src: []string{string(StateInit), string(StateGreeted)}, Dst: string(StateGreeted)},

			// MAIL FROM is only valid after greeting
			{Name: string(EventMailFrom), Src: []string{string(StateGreeted)}, Dst: string(StateMailFrom)},

			// RCPT TO is valid after MAIL FROM or after another RCPT TO (multiple recipients)
			{Name: string(EventRcptTo), Src: []string{string(StateMailFrom), string(StateRcptTo)}, Dst: string(StateRcptTo)},

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
		llfsm.Callbacks{
			// Log every state transition for observability
			"after_event": func(ctx context.Context, e *llfsm.Event) {
				s.log.Debug("smtp fsm transition",
					zap.String("session", s.id),
					zap.String("event", e.Event),
					zap.String("from", e.Src),
					zap.String("to", e.Dst),
				)
			},
		},
	)

	return s
}

// Handle runs the session read loop until the client disconnects or sends QUIT.
func (s *Session) Handle() {
	defer s.conn.Close()

	s.log.Info("smtp session started", zap.String("session", s.id))
	s.write(fmt.Sprintf("220 %s ESMTP gomail-core ready", s.cfg.SMTP.Domain))

	for {
		s.conn.SetReadDeadline(time.Now().Add(5 * time.Minute))

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

// write sends a response line to the client with CRLF as required by RFC 5321.
func (s *Session) write(line string) {
	fmt.Fprintf(s.conn, "%s\r\n", line)
	s.log.Debug("smtp sent", zap.String("session", s.id), zap.String("line", line))
}

// currentState returns the current FSM state.
func (s *Session) currentState() State {
	return State(s.fsm.Current())
}
