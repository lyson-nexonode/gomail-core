package imap

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

// State represents a single IMAP session state as defined in RFC 3501 section 3.
type State string

const (
	StateNotAuthenticated State = "not_authenticated"
	StateAuthenticated    State = "authenticated"
	StateSelected         State = "selected"
	StateLogout           State = "logout"
)

// IMAPEvent represents a FSM transition trigger.
type IMAPEvent string

const (
	EventLogin  IMAPEvent = "login"
	EventSelect IMAPEvent = "select"
	EventClose  IMAPEvent = "close"
	EventLogout IMAPEvent = "logout"
)

// Session represents a single IMAP client connection.
// It depends only on ports interfaces — never on concrete storage.
type Session struct {
	conn    net.Conn
	reader  *bufio.Reader
	writer  *bufio.Writer
	fsm     *llfsm.FSM
	cfg     *config.Config
	log     *zap.Logger
	id      string

	// readTimeout is the idle timeout per read. Zero means no deadline.
	// Use a non-zero value in production, zero in tests.
	readTimeout time.Duration

	userID   uint64
	username string
	selected *SelectedMailbox

	mailboxReader  ports.MailboxReader
	messageReader  ports.MessageReader
	domainResolver ports.DomainResolver
	userAuth       ports.UserAuthenticator
}

// NewSession creates a new IMAP session with a 30-minute read timeout.
func NewSession(
	conn net.Conn,
	cfg *config.Config,
	log *zap.Logger,
	mailboxReader ports.MailboxReader,
	messageReader ports.MessageReader,
	domainResolver ports.DomainResolver,
	userAuth ports.UserAuthenticator,
) *Session {
	return newSession(conn, cfg, log, mailboxReader, messageReader, domainResolver, userAuth, 30*time.Minute)
}

// newSession creates a session with a configurable read timeout.
// Use readTimeout=0 in tests to disable per-read deadlines.
func newSession(
	conn net.Conn,
	cfg *config.Config,
	log *zap.Logger,
	mailboxReader ports.MailboxReader,
	messageReader ports.MessageReader,
	domainResolver ports.DomainResolver,
	userAuth ports.UserAuthenticator,
	readTimeout time.Duration,
) *Session {
	s := &Session{
		conn:           conn,
		reader:         bufio.NewReader(conn),
		writer:         bufio.NewWriter(conn),
		cfg:            cfg,
		log:            log.With(zap.String("remote", conn.RemoteAddr().String())),
		id:             fmt.Sprintf("%d", time.Now().UnixNano()),
		readTimeout:    readTimeout,
		mailboxReader:  mailboxReader,
		messageReader:  messageReader,
		domainResolver: domainResolver,
		userAuth:       userAuth,
	}

	s.fsm = llfsm.NewFSM(
		string(StateNotAuthenticated),
		llfsm.Events{
			{Name: string(EventLogin), Src: []string{string(StateNotAuthenticated)}, Dst: string(StateAuthenticated)},
			{Name: string(EventSelect), Src: []string{string(StateAuthenticated)}, Dst: string(StateSelected)},
			{Name: string(EventClose), Src: []string{string(StateSelected)}, Dst: string(StateAuthenticated)},
			{Name: string(EventLogout), Src: []string{
				string(StateNotAuthenticated),
				string(StateAuthenticated),
				string(StateSelected),
			}, Dst: string(StateLogout)},
		},
		llfsm.Callbacks{
			"after_event": func(ctx context.Context, e *llfsm.Event) {
				s.log.Debug("imap fsm transition",
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

// Handle runs the session read loop until the client disconnects or sends LOGOUT.
func (s *Session) Handle() {
	defer func() { _ = s.conn.Close() }()

	s.log.Info("imap session started", zap.String("session", s.id))
	s.writeUntagged("OK", fmt.Sprintf("%s IMAP4rev1 gomail-core ready", s.cfg.SMTP.Domain))

	for {
		// Only set a read deadline if configured (production mode)
		if s.readTimeout > 0 {
			_ = s.conn.SetReadDeadline(time.Now().Add(s.readTimeout))
		}

		line, err := s.reader.ReadString('\n')
		if err != nil {
			s.log.Info("imap session closed", zap.String("session", s.id), zap.Error(err))
			return
		}

		line = strings.TrimRight(line, "\r\n")
		if line == "" {
			continue
		}

		s.log.Debug("imap received", zap.String("session", s.id), zap.String("line", line))

		tag, rest, ok := strings.Cut(line, " ")
		if !ok {
			_, _ = fmt.Fprintf(s.conn, "%s BAD Invalid command\r\n", line)
			continue
		}

		cmd, args, _ := strings.Cut(rest, " ")
		cmd = strings.ToUpper(cmd)

		s.dispatch(tag, cmd, args)

		if s.fsm.Current() == string(StateLogout) {
			return
		}
	}
}

// transition fires a FSM event and returns false if invalid.
func (s *Session) transition(event IMAPEvent) bool {
	if err := s.fsm.Event(context.Background(), string(event)); err != nil {
		s.log.Warn("imap invalid transition",
			zap.String("session", s.id),
			zap.String("event", string(event)),
			zap.String("current_state", s.fsm.Current()),
		)
		return false
	}
	return true
}

// transitionSelect handles SELECT and EXAMINE.
// Re-selecting a mailbox while already in selected state is valid (RFC 3501).
func (s *Session) transitionSelect() bool {
	if s.fsm.Current() == string(StateSelected) {
		return true
	}
	return s.transition(EventSelect)
}

// writeTagged sends a tagged response.
func (s *Session) writeTagged(tag, status, message string) {
	line := fmt.Sprintf("%s %s %s", tag, status, message)
	_, _ = fmt.Fprintf(s.conn, "%s\r\n", line)
	s.log.Debug("imap sent", zap.String("session", s.id), zap.String("line", line))
}

// writeUntagged sends an untagged response.
func (s *Session) writeUntagged(status, message string) {
	line := fmt.Sprintf("* %s %s", status, message)
	_, _ = fmt.Fprintf(s.conn, "%s\r\n", line)
	s.log.Debug("imap sent", zap.String("session", s.id), zap.String("line", line))
}

// writeLine sends a raw line with CRLF.
func (s *Session) writeLine(line string) {
	_, _ = fmt.Fprintf(s.conn, "%s\r\n", line)
	s.log.Debug("imap sent", zap.String("session", s.id), zap.String("line", line))
}

// dispatch routes a command to the appropriate handler based on the current FSM state.
// Commands that are invalid in the current state return a NO response.
func (s *Session) dispatch(tag, cmd, args string) {
	switch s.fsm.Current() {
	case string(StateNotAuthenticated):
		switch cmd {
		case "LOGIN":
			s.handleLogin(tag, args)
		case "CAPABILITY":
			s.handleCapability(tag)
		case "LOGOUT":
			s.handleLogout(tag)
		default:
			s.writeTagged(tag, "NO", "Command not allowed before authentication")
		}

	case string(StateAuthenticated):
		switch cmd {
		case "SELECT":
			s.handleSelect(tag, args, false)
		case "EXAMINE":
			s.handleSelect(tag, args, true)
		case "LIST":
			s.handleList(tag, args)
		case "LSUB":
			s.handleList(tag, args)
		case "CAPABILITY":
			s.handleCapability(tag)
		case "NOOP":
			s.writeTagged(tag, "OK", "NOOP completed")
		case "LOGOUT":
			s.handleLogout(tag)
		default:
			s.writeTagged(tag, "NO", "Command not allowed without selected mailbox")
		}

	case string(StateSelected):
		switch cmd {
		case "FETCH":
			s.handleFetch(tag, args)
		case "STORE":
			s.handleStore(tag, args)
		case "SEARCH":
			s.handleSearch(tag, args)
		case "EXPUNGE":
			s.handleExpunge(tag)
		case "CLOSE":
			s.handleClose(tag)
		case "SELECT":
			s.handleSelect(tag, args, false)
		case "EXAMINE":
			s.handleSelect(tag, args, true)
		case "LIST":
			s.handleList(tag, args)
		case "CAPABILITY":
			s.handleCapability(tag)
		case "NOOP":
			s.writeTagged(tag, "OK", "NOOP completed")
		case "LOGOUT":
			s.handleLogout(tag)
		default:
			s.writeTagged(tag, "BAD", "Unknown command")
		}
	}
}
