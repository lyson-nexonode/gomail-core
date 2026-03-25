package imap

import (
	"bufio"
	"context"
	"crypto/tls"
	"fmt"
	"strings"

	"go.uber.org/zap"
)

// handleCapability responds with the list of supported IMAP capabilities.
// Valid in any state (RFC 3501 section 6.1.1).
func (s *Session) handleCapability(tag string) {
	if s.tlsCfg != nil && !s.isTLS {
		caps := append(Capability, "STARTTLS")
		s.writeUntagged("CAPABILITY", strings.Join(caps, " "))
	} else {
		s.writeUntagged("CAPABILITY", strings.Join(Capability, " "))
	}
	s.writeTagged(tag, "OK", "CAPABILITY completed")
}

// handleLogin authenticates the user with username and password.
// Transitions the session from NotAuthenticated to Authenticated on success.
// RFC 3501 section 6.2.2.
func (s *Session) handleLogin(tag, args string) {
	parts := strings.SplitN(args, " ", 2)
	if len(parts) != 2 {
		s.writeTagged(tag, "BAD", "Syntax: LOGIN username password")
		return
	}

	username := strings.Trim(parts[0], "\"")
	password := strings.Trim(parts[1], "\"")

	localPart, domain, ok := parseAddress(username)
	if !ok {
		s.writeTagged(tag, "NO", "Invalid username format, expected user@domain")
		return
	}

	ctx := context.Background()

	d, err := s.domainResolver.FindDomain(ctx, domain)
	if err != nil {
		s.log.Warn("imap login unknown domain",
			zap.String("session", s.id),
			zap.String("domain", domain),
		)
		s.writeTagged(tag, "NO", "Invalid credentials")
		return
	}

	user, err := s.userAuth.FindUser(ctx, localPart, d.ID)
	if err != nil {
		s.log.Warn("imap login unknown user",
			zap.String("session", s.id),
			zap.String("username", username),
		)
		s.writeTagged(tag, "NO", "Invalid credentials")
		return
	}

	if !checkPassword(password, user.PasswordHash) {
		s.log.Warn("imap login invalid password",
			zap.String("session", s.id),
			zap.String("username", username),
		)
		s.writeTagged(tag, "NO", "Invalid credentials")
		return
	}

	if !s.transition(EventLogin) {
		s.writeTagged(tag, "NO", "Login failed")
		return
	}

	s.userID = user.ID
	s.username = username

	s.log.Info("imap login successful",
		zap.String("session", s.id),
		zap.String("username", username),
	)

	s.writeTagged(tag, "OK", "LOGIN completed")
}

// handleSelect opens a mailbox for read-write or read-only access.
// Transitions the session to the Selected state (RFC 3501 section 6.3.1).
func (s *Session) handleSelect(tag, args string, readOnly bool) {
	name := strings.Trim(strings.TrimSpace(args), "\"")
	if name == "" {
		s.writeTagged(tag, "BAD", "Syntax: SELECT mailbox")
		return
	}

	ctx := context.Background()

	mb, err := s.mailboxReader.FindMailbox(ctx, s.userID, name)
	if err != nil {
		s.log.Warn("imap select mailbox not found",
			zap.String("session", s.id),
			zap.String("mailbox", name),
		)
		s.writeTagged(tag, "NO", fmt.Sprintf("Mailbox %q does not exist", name))
		return
	}

	messages, err := s.messageReader.ListMessages(ctx, mb.ID)
	if err != nil {
		s.writeTagged(tag, "NO", "Error reading mailbox")
		return
	}

	if !s.transitionSelect() {
		s.writeTagged(tag, "NO", "Cannot select mailbox")
		return
	}

	s.selected = &SelectedMailbox{
		ID:          mb.ID,
		Name:        mb.Name,
		UIDValidity: mb.UIDValidity,
		UIDNext:     mb.UIDNext,
		ReadOnly:    readOnly,
	}

	// Send required SELECT responses as per RFC 3501 section 7.3.1
	s.writeUntagged(fmt.Sprintf("%d", len(messages)), "EXISTS")
	s.writeUntagged("0", "RECENT")
	s.writeLine("* OK [UNSEEN 0] No unseen messages")
	s.writeLine(fmt.Sprintf("* OK [UIDVALIDITY %d] UIDs valid", mb.UIDValidity))
	s.writeLine(fmt.Sprintf("* OK [UIDNEXT %d] Predicted next UID", mb.UIDNext))
	s.writeLine("* FLAGS (\\Seen \\Answered \\Flagged \\Deleted \\Draft)")
	s.writeLine("* OK [PERMANENTFLAGS (\\Seen \\Answered \\Flagged \\Deleted \\Draft \\*)] Permanent flags")

	if readOnly {
		s.writeTagged(tag, "OK", "[READ-ONLY] EXAMINE completed")
	} else {
		s.writeTagged(tag, "OK", "[READ-WRITE] SELECT completed")
	}

	s.log.Info("imap mailbox selected",
		zap.String("session", s.id),
		zap.String("mailbox", name),
		zap.Int("messages", len(messages)),
	)
}

// handleList returns a list of mailboxes matching the given pattern.
// RFC 3501 section 6.3.8.
func (s *Session) handleList(tag, args string) {
	parts := strings.SplitN(args, " ", 2)
	if len(parts) != 2 {
		s.writeTagged(tag, "BAD", "Syntax: LIST reference pattern")
		return
	}

	pattern := strings.Trim(parts[1], "\"")
	ctx := context.Background()

	mailboxes, err := s.mailboxReader.ListMailboxes(ctx, s.userID)
	if err != nil {
		s.writeTagged(tag, "NO", "Error listing mailboxes")
		return
	}

	for _, mb := range mailboxes {
		if pattern == "*" || pattern == "%" || strings.EqualFold(mb.Name, pattern) {
			s.writeLine(fmt.Sprintf(`* LIST (\HasNoChildren) "/" %q`, mb.Name))
		}
	}

	s.writeTagged(tag, "OK", "LIST completed")
}

// handleFetch retrieves message data for the given sequence set.
// RFC 3501 section 6.4.5.
func (s *Session) handleFetch(tag, args string) {
	if s.selected == nil {
		s.writeTagged(tag, "BAD", "No mailbox selected")
		return
	}

	parts := strings.SplitN(args, " ", 2)
	if len(parts) != 2 {
		s.writeTagged(tag, "BAD", "Syntax: FETCH sequence data-items")
		return
	}

	ctx := context.Background()

	messages, err := s.messageReader.ListMessages(ctx, s.selected.ID)
	if err != nil {
		s.writeTagged(tag, "NO", "Error fetching messages")
		return
	}

	items := strings.ToUpper(parts[1])

	for i, msg := range messages {
		seqNum := i + 1

		var response strings.Builder
		fmt.Fprintf(&response, "* %d FETCH (", seqNum)

		if strings.Contains(items, "FLAGS") {
			fmt.Fprintf(&response, "FLAGS (%s) ", msg.Flags)
		}

		if strings.Contains(items, "UID") {
			fmt.Fprintf(&response, "UID %d ", msg.UID)
		}

		if strings.Contains(items, "RFC822.SIZE") {
			fmt.Fprintf(&response, "RFC822.SIZE %d ", msg.SizeBytes)
		}

		if strings.Contains(items, "BODY[]") || strings.Contains(items, "RFC822") {
			// Try Redis cache first, fall back to placeholder
			body, err := s.messageReader.GetMessageBody(ctx, msg.ID)
			if err != nil || body == nil {
				body = []byte("(message body unavailable)")
			}
			fmt.Fprintf(&response, "BODY[] {%d}\r\n%s ", len(body), string(body))
		}

		result := strings.TrimRight(response.String(), " ") + ")"
		s.writeLine(result)
	}

	s.writeTagged(tag, "OK", "FETCH completed")
}

// handleStore updates message flags.
// RFC 3501 section 6.4.6.
func (s *Session) handleStore(tag, args string) {
	if s.selected == nil {
		s.writeTagged(tag, "BAD", "No mailbox selected")
		return
	}

	if s.selected.ReadOnly {
		s.writeTagged(tag, "NO", "[READ-ONLY] Cannot store flags in read-only mailbox")
		return
	}

	// TODO: implement flag updates in MySQL
	s.writeTagged(tag, "OK", "STORE completed")
}

// handleSearch searches for messages matching the given criteria.
// RFC 3501 section 6.4.4.
func (s *Session) handleSearch(tag, args string) {
	if s.selected == nil {
		s.writeTagged(tag, "BAD", "No mailbox selected")
		return
	}

	ctx := context.Background()

	messages, err := s.messageReader.ListMessages(ctx, s.selected.ID)
	if err != nil {
		s.writeTagged(tag, "NO", "Error searching messages")
		return
	}

	// Return all sequence numbers for now
	// TODO: implement full search criteria parsing
	var seqNums []string
	for i := range messages {
		seqNums = append(seqNums, fmt.Sprintf("%d", i+1))
	}

	s.writeLine(fmt.Sprintf("* SEARCH %s", strings.Join(seqNums, " ")))
	s.writeTagged(tag, "OK", "SEARCH completed")
}

// handleExpunge removes all messages flagged as \Deleted.
// RFC 3501 section 6.4.3.
func (s *Session) handleExpunge(tag string) {
	if s.selected == nil {
		s.writeTagged(tag, "BAD", "No mailbox selected")
		return
	}

	if s.selected.ReadOnly {
		s.writeTagged(tag, "NO", "[READ-ONLY] Cannot expunge read-only mailbox")
		return
	}

	// TODO: implement message deletion in MySQL
	s.writeTagged(tag, "OK", "EXPUNGE completed")
}

// handleClose closes the selected mailbox and returns to Authenticated state.
// RFC 3501 section 6.4.2.
func (s *Session) handleClose(tag string) {
	s.selected = nil
	s.transition(EventClose)
	s.writeTagged(tag, "OK", "CLOSE completed")
}

// handleLogout closes the session gracefully.
// RFC 3501 section 6.1.3.
func (s *Session) handleLogout(tag string) {
	s.transition(EventLogout)
	s.writeUntagged("BYE", "gomail-core logging out")
	s.writeTagged(tag, "OK", "LOGOUT completed")
	s.log.Info("imap session logout", zap.String("session", s.id))
}

// parseAddress splits "user@domain" into local part and domain.
func parseAddress(addr string) (string, string, bool) {
	parts := strings.SplitN(addr, "@", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", "", false
	}
	return parts[0], parts[1], true
}

// handleSTARTTLS upgrades the plain IMAP connection to TLS (RFC 2595).
// After the upgrade the client must re-issue CAPABILITY.
func (s *Session) handleSTARTTLS(tag string) {
	if s.isTLS {
		s.writeTagged(tag, "NO", "Already using TLS")
		return
	}

	if s.tlsCfg == nil {
		s.writeTagged(tag, "NO", "TLS not available")
		return
	}

	s.writeTagged(tag, "OK", "Begin TLS negotiation now")

	// Wrap the existing connection in TLS
	tlsConn := tls.Server(s.conn, s.tlsCfg)
	if err := tlsConn.Handshake(); err != nil {
		s.log.Error("imap starttls handshake failed",
			zap.String("session", s.id),
			zap.Error(err),
		)
		return
	}

	// Replace connection and reader with TLS-wrapped versions
	s.conn = tlsConn
	s.reader = bufio.NewReader(tlsConn)
	s.isTLS = true

	s.log.Info("imap starttls upgrade successful", zap.String("session", s.id))
}
