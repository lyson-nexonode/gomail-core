package smtp

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

// mockDelivery is a test double for ports.DeliveryPipeline.
type mockDelivery struct {
	delivered []ports.MessageReceived
	err       error
}

func (m *mockDelivery) Deliver(_ context.Context, event ports.MessageReceived) error {
	if m.err != nil {
		return m.err
	}
	m.delivered = append(m.delivered, event)
	return nil
}

// newTestSession creates a Session connected to an in-memory pipe for testing.
func newTestSession(t *testing.T, delivery ports.DeliveryPipeline) (*Session, net.Conn) {
	t.Helper()

	server, client := net.Pipe()

	cfg := &config.Config{
		SMTP: config.SMTPConfig{
			Domain:  "gomail.local",
			MaxSize: 26214400,
		},
	}

	log, _ := zap.NewDevelopment()
	session := NewSession(server, cfg, log, delivery)
	return session, client
}

// sendRecv sends a command and reads the response from the client side.

// readLines reads multiple response lines until a final response line.

// TestSMTPSessionGreeting verifies that the server sends a 220 greeting on connect.
func TestSMTPSessionGreeting(t *testing.T) {
	delivery := &mockDelivery{}
	session, client := newTestSession(t, delivery)

	go session.Handle()
	defer func() { _ = client.Close() }()

	_ = client.SetDeadline(time.Now().Add(2 * time.Second))
	reader := bufio.NewReader(client)
	greeting, err := reader.ReadString('\n')
	require.NoError(t, err)
	assert.True(t, strings.HasPrefix(greeting, "220"))
}

// TestSMTPSessionEHLO verifies the EHLO response includes capabilities.
func TestSMTPSessionEHLO(t *testing.T) {
	delivery := &mockDelivery{}
	session, client := newTestSession(t, delivery)

	go session.Handle()
	defer func() { _ = client.Close() }()

	// Read greeting
	_ = client.SetDeadline(time.Now().Add(2 * time.Second))
	reader := bufio.NewReader(client)
	_, _ = reader.ReadString('\n')

	// Send EHLO
	_, _ = fmt.Fprintf(client, "EHLO testclient\r\n")

	// Read multi-line response
	var lines []string
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			break
		}
		line = strings.TrimRight(line, "\r\n")
		lines = append(lines, line)
		if len(line) >= 4 && line[3] == ' ' {
			break
		}
	}

	assert.NotEmpty(t, lines)
	assert.True(t, strings.HasPrefix(lines[0], "250"))
}

// TestSMTPSessionQUIT verifies that QUIT closes the session gracefully.
func TestSMTPSessionQUIT(t *testing.T) {
	delivery := &mockDelivery{}
	session, client := newTestSession(t, delivery)

	go session.Handle()
	defer func() { _ = client.Close() }()

	_ = client.SetDeadline(time.Now().Add(2 * time.Second))
	reader := bufio.NewReader(client)

	// Read greeting
	_, _ = reader.ReadString('\n')

	// Send QUIT
	_, _ = fmt.Fprintf(client, "QUIT\r\n")
	resp, err := reader.ReadString('\n')
	require.NoError(t, err)
	assert.True(t, strings.HasPrefix(strings.TrimRight(resp, "\r\n"), "221"))
}

// TestSMTPSessionInvalidCommand verifies that unknown commands return 500.
func TestSMTPSessionInvalidCommand(t *testing.T) {
	delivery := &mockDelivery{}
	session, client := newTestSession(t, delivery)

	go session.Handle()
	defer func() { _ = client.Close() }()

	_ = client.SetDeadline(time.Now().Add(2 * time.Second))
	reader := bufio.NewReader(client)

	// Read greeting
	_, _ = reader.ReadString('\n')

	// Send invalid command
	_, _ = fmt.Fprintf(client, "INVALID\r\n")
	resp, err := reader.ReadString('\n')
	require.NoError(t, err)
	assert.True(t, strings.HasPrefix(strings.TrimRight(resp, "\r\n"), "500"))
}

// TestSMTPSessionFullTransaction verifies a complete email send cycle.
func TestSMTPSessionFullTransaction(t *testing.T) {
	delivery := &mockDelivery{}
	session, client := newTestSession(t, delivery)

	go session.Handle()
	defer func() { _ = client.Close() }()

	_ = client.SetDeadline(time.Now().Add(5 * time.Second))
	reader := bufio.NewReader(client)
	writer := bufio.NewWriter(client)

	readLine := func() string {
		// Read all continuation lines
		var last string
		for {
			line, _ := reader.ReadString('\n')
			line = strings.TrimRight(line, "\r\n")
			last = line
			if len(line) < 4 || line[3] == ' ' {
				break
			}
		}
		return last
	}

	send := func(cmd string) string {
		_, _ = fmt.Fprintf(writer, "%s\r\n", cmd)
		_ = writer.Flush()
		return readLine()
	}

	// Read greeting
	readLine()

	assert.True(t, strings.HasPrefix(send("EHLO testclient"), "250"))
	assert.True(t, strings.HasPrefix(send("MAIL FROM:<alice@gomail.local>"), "250"))
	assert.True(t, strings.HasPrefix(send("RCPT TO:<test@gomail.local>"), "250"))

	// Send DATA command
	resp := send("DATA")
	assert.True(t, strings.HasPrefix(resp, "354"))

	// Send message body
	_, _ = fmt.Fprintf(writer, "From: alice@gomail.local\r\n")
	_, _ = fmt.Fprintf(writer, "To: test@gomail.local\r\n")
	_, _ = fmt.Fprintf(writer, "Subject: Test\r\n")
	_, _ = fmt.Fprintf(writer, "\r\n")
	_, _ = fmt.Fprintf(writer, "Hello!\r\n")
	_, _ = fmt.Fprintf(writer, ".\r\n")
	_ = writer.Flush()

	resp = readLine()
	assert.True(t, strings.HasPrefix(resp, "250"), "expected 250 after DATA, got: %s", resp)

	// Verify delivery was called
	assert.Len(t, delivery.delivered, 1)
	assert.Equal(t, "alice@gomail.local", delivery.delivered[0].From)
	assert.Contains(t, delivery.delivered[0].To, "test@gomail.local")

	assert.True(t, strings.HasPrefix(send("QUIT"), "221"))
}

// TestSMTPSessionRSET verifies that RSET resets the transaction.
func TestSMTPSessionRSET(t *testing.T) {
	delivery := &mockDelivery{}
	session, client := newTestSession(t, delivery)

	go session.Handle()
	defer func() { _ = client.Close() }()

	_ = client.SetDeadline(time.Now().Add(2 * time.Second))
	reader := bufio.NewReader(client)
	writer := bufio.NewWriter(client)

	readLine := func() string {
		var last string
		for {
			line, _ := reader.ReadString('\n')
			line = strings.TrimRight(line, "\r\n")
			last = line
			if len(line) < 4 || line[3] == ' ' {
				break
			}
		}
		return last
	}

	send := func(cmd string) string {
		_, _ = fmt.Fprintf(writer, "%s\r\n", cmd)
		_ = writer.Flush()
		return readLine()
	}

	readLine() // greeting

	assert.True(t, strings.HasPrefix(send("EHLO testclient"), "250"))
	assert.True(t, strings.HasPrefix(send("MAIL FROM:<alice@gomail.local>"), "250"))
	assert.True(t, strings.HasPrefix(send("RSET"), "250"))

	// After RSET, MAIL FROM should work again
	assert.True(t, strings.HasPrefix(send("MAIL FROM:<bob@gomail.local>"), "250"))
}

// TestSMTPSessionNOOP verifies that NOOP returns 250.
func TestSMTPSessionNOOP(t *testing.T) {
	delivery := &mockDelivery{}
	session, client := newTestSession(t, delivery)

	go session.Handle()
	defer func() { _ = client.Close() }()

	_ = client.SetDeadline(time.Now().Add(2 * time.Second))
	reader := bufio.NewReader(client)

	_, _ = reader.ReadString('\n') // greeting

	_, _ = fmt.Fprintf(client, "NOOP\r\n")
	resp, _ := reader.ReadString('\n')
	assert.True(t, strings.HasPrefix(strings.TrimRight(resp, "\r\n"), "250"))
}
