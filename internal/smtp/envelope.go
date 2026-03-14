package smtp

import "time"

// Envelope holds the data collected during a single SMTP transaction.
// It is built progressively as the client sends MAIL FROM, RCPT TO and DATA commands.
// A new envelope is created for each transaction and reset on RSET.
type Envelope struct {
	// From is the sender address extracted from MAIL FROM.
	From string

	// To holds all recipient addresses collected from RCPT TO commands.
	// A single transaction can have multiple recipients.
	To []string

	// Data holds the raw message body received after the DATA command.
	// It includes headers and body separated by a blank line (RFC 5322).
	Data []byte

	// Size is the total byte count of the message body.
	Size int64

	// ReceivedAt is the timestamp when the DATA transfer completed.
	ReceivedAt time.Time
}

// AddRecipient appends a recipient address to the envelope.
func (e *Envelope) AddRecipient(addr string) {
	e.To = append(e.To, addr)
}

// HasRecipients returns true if at least one recipient has been added.
func (e *Envelope) HasRecipients() bool {
	return len(e.To) > 0
}

// Reset clears the envelope for reuse within the same session (RSET command).
// The session itself remains open and authenticated.
func (e *Envelope) Reset() {
	e.From = ""
	e.To = nil
	e.Data = nil
	e.Size = 0
	e.ReceivedAt = time.Time{}
}
