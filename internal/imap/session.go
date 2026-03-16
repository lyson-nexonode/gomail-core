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
	// StateNotAuthenticated is the initial state after connection.
	// Only LOGIN, CAPABILITY and LOGOUT are allowed.
	StateNotAuthenticated State = "not_authenticated"

	// StateAuthenticated is entered after successful LOGIN.
	// The client can list and select mailboxes.
	StateAuthenticated State = "authenticated"

	// StateSelected is entered after SELECT or EXAMINE.
	// The client can fetch, store, search and expunge messages.
	StateSelected State = "selected"

	// StateLogout is the terminal state after LOGOUT.
	StateLogout State = "logout"
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
	conn   net.Conn
	reader *bufio.Reader
	writer *bufio.Writer
	fsm    *llfsm.FSM
	cfg    *config.Config
	log    *zap.Logger
	id     string

	// authenticated user — set after successful LOGIN
	userID   uint64
	username string

	// currently selected mailbox — set after SELECT or EXAMINE
	selected *SelectedMailbox

	// storage ports
	mailboxReader  ports.MailboxReader
	messageReader  ports.MessageReader
	domainResolver ports.DomainResolver
	userAuth       ports.UserAuthenticator
}

// NewSession creates a new IMAP session for the given connection.
func NewSession(
	conn net.Conn,
	cfg *config.Config,
	log *zap.Logger,
	mailboxReader ports.MailboxReader,
	messageReader ports.MessageReader,
	domainResolver ports.DomainResolver,
	userAuth ports.UserAuthenticator,
) *Session {
	s := &Session{
		conn:           conn,
		reader:         bufio.NewReader(conn),
		writer:         bufio.NewWriter(conn),
		cfg:            cfg,
		log:            log.With(zap.String("remote", conn.RemoteAddr().String())),
		id:             fmt.Sprintf("%d", time.Now().UnixNano()),
		mailboxReader:  mailboxReader,
		messageReader:  messageReader,
		domainResolver: domainResolver,
		userAuth:       userAuth,
	}

	s.fsm = llfsm.NewFSM(
		string(StateNotAuthenticated),
		llfsm.Events{
			// LOGIN transitions from not_authenticated to authenticated
			{Name: string(EventLogin), Src: []string{string(StateNotAuthenticated)}, Dst: string(StateAuthenticated)},

			// SELECT and EXAMINE transition from authenticated to selected
			{Name: string(EventSelect), Src: []string{string(StateAuthenticated), string(StateSelected)}, Dst: string(StateSelected)},

			// CLOSE goes back to authenticated without expunging
			{Name: string(EventClose), Src: []string{string(StateSelected)}, Dst: string(StateAuthenticated)},

			// LOGOUT is valid from any state
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

	// Send server greeting as required by RFC 3501 section 7.1.5
	s.writeUntagged("OK", fmt.Sprintf("%s IMAP4rev1 gomail-core ready", s.cfg.SMTP.Domain))

	for {
		_ = s.conn.SetReadDeadline(time.Now().Add(30 * time.Minute))

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

		// IMAP commands are prefixed with a client tag: "A001 LOGIN user pass"
		tag, rest, ok := strings.Cut(line, " ")
		if !ok {
			s.writeTagged(line, "BAD", "Invalid command")
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
			s.handleList(tag, args) // LSUB uses same logic as LIST for now
		case "CAPABILITY":
			s.handleCapability(tag)
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

// writeTagged sends a tagged response: "A001 OK message\r\n"
func (s *Session) writeTagged(tag, status, message string) {
	line := fmt.Sprintf("%s %s %s", tag, status, message)
	_, _ = fmt.Fprintf(s.conn, "%s", line)
	s.log.Debug("imap sent", zap.String("session", s.id), zap.String("line", line))
}

// writeUntagged sends an untagged response: "* OK message\r\n"
func (s *Session) writeUntagged(status, message string) {
	line := fmt.Sprintf("* %s %s", status, message)
	_, _ = fmt.Fprintf(s.conn, "%s", line)
	s.log.Debug("imap sent", zap.String("session", s.id), zap.String("line", line))
}

// writeLine sends a raw line with CRLF.
func (s *Session) writeLine(line string) {
	_, _ = fmt.Fprintf(s.conn, "%s", line)
	s.log.Debug("imap sent", zap.String("session", s.id), zap.String("line", line))
}

// transitionSelect handles SELECT and EXAMINE.
// Re-selecting a mailbox while already in selected state is valid (RFC 3501).
// looplab/fsm does not support self-transitions so we handle this explicitly.
func (s *Session) transitionSelect() bool {
	if s.fsm.Current() == string(StateSelected) {
		// Already selected — switching mailbox is valid without FSM transition
		return true
	}
	return s.transition(EventSelect)
}
