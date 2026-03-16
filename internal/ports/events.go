package ports

import "time"

// Event is the base type for all domain events.
type Event interface {
	EventName() string
	OccurredAt() time.Time
}

// MessageReceived is published when a complete message has been received via SMTP.
type MessageReceived struct {
	From      string
	To        []string
	Body      []byte
	Size      int64
	Timestamp time.Time
}

func (e MessageReceived) EventName() string     { return "message.received" }
func (e MessageReceived) OccurredAt() time.Time { return e.Timestamp }

// MessageDelivered is published when a message has been persisted to a mailbox.
type MessageDelivered struct {
	MessageID uint64
	MailboxID uint64
	UID       uint32
	To        string
	Timestamp time.Time
}

func (e MessageDelivered) EventName() string     { return "message.delivered" }
func (e MessageDelivered) OccurredAt() time.Time { return e.Timestamp }
