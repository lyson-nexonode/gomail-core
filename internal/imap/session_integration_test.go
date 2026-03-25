package imap

import (
	"bufio"
	"context"
	"fmt"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/lyson-nexonode/gomail-core/config"
	"github.com/lyson-nexonode/gomail-core/internal/ports"
)

type mockMailboxReader struct {
	mailboxes []ports.Mailbox
	err       error
}

func (m *mockMailboxReader) FindMailbox(_ context.Context, _ uint64, name string) (*ports.Mailbox, error) {
	if m.err != nil {
		return nil, m.err
	}
	for _, mb := range m.mailboxes {
		if mb.Name == name {
			cp := mb
			return &cp, nil
		}
	}
	return nil, fmt.Errorf("mailbox %q not found", name)
}

func (m *mockMailboxReader) ListMailboxes(_ context.Context, _ uint64) ([]ports.Mailbox, error) {
	return m.mailboxes, m.err
}

func (m *mockMailboxReader) GetMailboxByID(_ context.Context, id uint64) (*ports.Mailbox, error) {
	for _, mb := range m.mailboxes {
		if mb.ID == id {
			cp := mb
			return &cp, nil
		}
	}
	return nil, fmt.Errorf("mailbox %d not found", id)
}

type mockMessageReader struct {
	messages []ports.Message
	body     []byte
	err      error
}

func (m *mockMessageReader) GetMessageBody(_ context.Context, _ uint64) ([]byte, error) {
	return m.body, m.err
}

func (m *mockMessageReader) ListMessages(_ context.Context, _ uint64) ([]ports.Message, error) {
	return m.messages, m.err
}

func (m *mockMessageReader) GetMessageByUID(_ context.Context, _ uint64, _ uint32) (*ports.Message, error) {
	if len(m.messages) > 0 {
		return &m.messages[0], nil
	}
	return nil, fmt.Errorf("not found")
}

type mockDomainResolver struct {
	domain *ports.Domain
	err    error
}

func (m *mockDomainResolver) FindDomain(_ context.Context, _ string) (*ports.Domain, error) {
	return m.domain, m.err
}

type mockUserAuth struct {
	user *ports.User
	err  error
}

func (m *mockUserAuth) FindUser(_ context.Context, _ string, _ uint64) (*ports.User, error) {
	return m.user, m.err
}

const validHash = "$2a$10$92IXUNpkjO0rOQ5byMi.Ye4oKoEa3Ro9llC/.og/at2.uheWG/igi"

func defaultMocks() (*mockMailboxReader, *mockMessageReader, *mockDomainResolver, *mockUserAuth) {
	return &mockMailboxReader{
			mailboxes: []ports.Mailbox{
				{ID: 1, UserID: 1, Name: "INBOX", UIDValidity: 1, UIDNext: 2},
				{ID: 2, UserID: 1, Name: "Sent", UIDValidity: 1, UIDNext: 1},
			},
		},
		&mockMessageReader{
			messages: []ports.Message{
				{ID: 1, MailboxID: 1, UID: 1, Flags: "", SizeBytes: 100,
					EnvelopeFrom: "alice@gomail.local", EnvelopeTo: "test@gomail.local",
					Subject: "Test"},
			},
			body: []byte("From: alice@gomail.local\r\nSubject: Test\r\n\r\nHello!"),
		},
		&mockDomainResolver{domain: &ports.Domain{ID: 1, Name: "gomail.local"}},
		&mockUserAuth{user: &ports.User{
			ID: 1, DomainID: 1, Username: "test", PasswordHash: validHash,
		}}
}

// newIMAPTestClient creates an IMAP session over a real TCP connection.
// It waits for the server goroutine to be ready before dialing.
func newIMAPTestClient(t *testing.T, mb *mockMailboxReader, msg *mockMessageReader, dr *mockDomainResolver, ua *mockUserAuth) *imapTestClient {
	t.Helper()

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)

	cfg := &config.Config{SMTP: config.SMTPConfig{Domain: "gomail.local"}}
	log, _ := zap.NewDevelopment()

	// ready signals that Accept() returned and the session is started
	ready := make(chan struct{})

	go func() {
		defer func() { _ = ln.Close() }()
		conn, err := ln.Accept()
		if err != nil {
			close(ready)
			return
		}
		close(ready)
		s := newSession(conn, cfg, log, mb, msg, dr, ua, 0, nil, false)
		s.Handle()
	}()

	// Dial before waiting for ready — TCP handshake queues the connection
	clientConn, err := net.DialTimeout("tcp", ln.Addr().String(), 2*time.Second)
	require.NoError(t, err)
	t.Cleanup(func() { _ = clientConn.Close() })

	// Wait for Accept() to have processed the connection
	select {
	case <-ready:
	case <-time.After(2 * time.Second):
		t.Fatal("server did not accept connection in time")
	}

	c := &imapTestClient{
		reader: bufio.NewReader(clientConn),
		writer: bufio.NewWriter(clientConn),
		conn:   clientConn,
		t:      t,
	}

	// Read greeting — server sends it immediately after Accept
	clientConn.SetDeadline(time.Now().Add(3 * time.Second)) //nolint:errcheck
	greeting, err := c.reader.ReadString('\n')
	require.NoError(t, err, "failed to read greeting")
	require.True(t, strings.HasPrefix(strings.TrimRight(greeting, "\r\n"), "* OK"),
		"expected greeting, got: %s", greeting)

	return c
}

type imapTestClient struct {
	reader *bufio.Reader
	writer *bufio.Writer
	conn   net.Conn
	t      *testing.T
	seq    int
}

// send sends a tagged IMAP command and returns the final tagged response.
func (c *imapTestClient) send(cmd string) string {
	c.t.Helper()
	c.seq++
	tag := fmt.Sprintf("T%03d", c.seq)

	c.conn.SetDeadline(time.Now().Add(3 * time.Second)) //nolint:errcheck
	_, err := fmt.Fprintf(c.writer, "%s %s\r\n", tag, cmd)
	require.NoError(c.t, err)
	require.NoError(c.t, c.writer.Flush())

	for {
		c.conn.SetDeadline(time.Now().Add(3 * time.Second)) //nolint:errcheck
		line, err := c.reader.ReadString('\n')
		require.NoError(c.t, err, "reading response to %q", cmd)
		line = strings.TrimRight(line, "\r\n")
		if strings.HasPrefix(line, tag) {
			return line
		}
	}
}

func TestIMAPSessionGreeting(t *testing.T) {
	mb, msg, dr, ua := defaultMocks()
	c := newIMAPTestClient(t, mb, msg, dr, ua)
	// greeting already validated in newIMAPTestClient
	assert.Contains(t, c.send("LOGOUT"), "OK")
}

func TestIMAPSessionCapability(t *testing.T) {
	mb, msg, dr, ua := defaultMocks()
	c := newIMAPTestClient(t, mb, msg, dr, ua)
	assert.Contains(t, c.send("CAPABILITY"), "OK")
}

func TestIMAPSessionLoginSuccess(t *testing.T) {
	mb, msg, dr, ua := defaultMocks()
	c := newIMAPTestClient(t, mb, msg, dr, ua)
	assert.Contains(t, c.send("LOGIN test@gomail.local password"), "OK")
}

func TestIMAPSessionLoginWrongPassword(t *testing.T) {
	mb, msg, dr, ua := defaultMocks()
	c := newIMAPTestClient(t, mb, msg, dr, ua)
	assert.Contains(t, c.send("LOGIN test@gomail.local wrongpassword"), "NO")
}

func TestIMAPSessionLoginUnknownDomain(t *testing.T) {
	mb, msg, _, ua := defaultMocks()
	dr := &mockDomainResolver{err: fmt.Errorf("not found")}
	c := newIMAPTestClient(t, mb, msg, dr, ua)
	assert.Contains(t, c.send("LOGIN test@unknown.com password"), "NO")
}

func TestIMAPSessionCommandBeforeAuth(t *testing.T) {
	mb, msg, dr, ua := defaultMocks()
	c := newIMAPTestClient(t, mb, msg, dr, ua)
	assert.Contains(t, c.send("SELECT INBOX"), "NO")
}

func TestIMAPSessionList(t *testing.T) {
	mb, msg, dr, ua := defaultMocks()
	c := newIMAPTestClient(t, mb, msg, dr, ua)
	require.Contains(t, c.send("LOGIN test@gomail.local password"), "OK")
	assert.Contains(t, c.send(`LIST "" "*"`), "OK")
}

func TestIMAPSessionSelect(t *testing.T) {
	mb, msg, dr, ua := defaultMocks()
	c := newIMAPTestClient(t, mb, msg, dr, ua)
	require.Contains(t, c.send("LOGIN test@gomail.local password"), "OK")
	assert.Contains(t, c.send("SELECT INBOX"), "OK")
}

func TestIMAPSessionExamine(t *testing.T) {
	mb, msg, dr, ua := defaultMocks()
	c := newIMAPTestClient(t, mb, msg, dr, ua)
	require.Contains(t, c.send("LOGIN test@gomail.local password"), "OK")
	assert.Contains(t, c.send("EXAMINE INBOX"), "READ-ONLY")
}

func TestIMAPSessionFetch(t *testing.T) {
	mb, msg, dr, ua := defaultMocks()
	c := newIMAPTestClient(t, mb, msg, dr, ua)
	require.Contains(t, c.send("LOGIN test@gomail.local password"), "OK")
	require.Contains(t, c.send("SELECT INBOX"), "OK")
	assert.Contains(t, c.send("FETCH 1 (FLAGS UID RFC822.SIZE)"), "OK")
}

func TestIMAPSessionSearch(t *testing.T) {
	mb, msg, dr, ua := defaultMocks()
	c := newIMAPTestClient(t, mb, msg, dr, ua)
	require.Contains(t, c.send("LOGIN test@gomail.local password"), "OK")
	require.Contains(t, c.send("SELECT INBOX"), "OK")
	assert.Contains(t, c.send("SEARCH ALL"), "OK")
}

func TestIMAPSessionClose(t *testing.T) {
	mb, msg, dr, ua := defaultMocks()
	c := newIMAPTestClient(t, mb, msg, dr, ua)
	require.Contains(t, c.send("LOGIN test@gomail.local password"), "OK")
	require.Contains(t, c.send("SELECT INBOX"), "OK")
	assert.Contains(t, c.send("CLOSE"), "OK")
}

func TestIMAPSessionLogout(t *testing.T) {
	mb, msg, dr, ua := defaultMocks()
	c := newIMAPTestClient(t, mb, msg, dr, ua)
	assert.Contains(t, c.send("LOGOUT"), "OK")
}

func TestIMAPSessionNOOP(t *testing.T) {
	mb, msg, dr, ua := defaultMocks()
	c := newIMAPTestClient(t, mb, msg, dr, ua)
	require.Contains(t, c.send("LOGIN test@gomail.local password"), "OK")
	assert.Contains(t, c.send("NOOP"), "OK")
}

func TestIMAPSessionUnknownCommand(t *testing.T) {
	mb, msg, dr, ua := defaultMocks()
	c := newIMAPTestClient(t, mb, msg, dr, ua)
	require.Contains(t, c.send("LOGIN test@gomail.local password"), "OK")
	require.Contains(t, c.send("SELECT INBOX"), "OK")
	assert.Contains(t, c.send("BADCMD"), "BAD")
}
