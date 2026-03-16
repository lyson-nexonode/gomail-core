package ports

import "context"

// DeliveryPipeline is the port that the SMTP server uses to hand off
// a received message for persistence and routing.
type DeliveryPipeline interface {
	Deliver(ctx context.Context, event MessageReceived) error
}

// MailboxReader defines read operations on mailboxes.
type MailboxReader interface {
	FindMailbox(ctx context.Context, userID uint64, name string) (*Mailbox, error)
	ListMailboxes(ctx context.Context, userID uint64) ([]Mailbox, error)
	GetMailboxByID(ctx context.Context, id uint64) (*Mailbox, error)
}

// MessageReader defines read operations for message bodies.
type MessageReader interface {
	GetMessageBody(ctx context.Context, messageID uint64) ([]byte, error)
	ListMessages(ctx context.Context, mailboxID uint64) ([]Message, error)
	GetMessageByUID(ctx context.Context, mailboxID uint64, uid uint32) (*Message, error)
}

// DomainResolver checks if a domain is managed by this server.
type DomainResolver interface {
	FindDomain(ctx context.Context, name string) (*Domain, error)
}

// UserAuthenticator validates user credentials.
type UserAuthenticator interface {
	FindUser(ctx context.Context, username string, domainID uint64) (*User, error)
}

// Domain is the domain model for a managed mail domain.
type Domain struct {
	ID   uint64
	Name string
}

// User is the domain model for a mail account.
type User struct {
	ID           uint64
	DomainID     uint64
	Username     string
	PasswordHash string
	QuotaBytes   int64
}

// Mailbox is the domain model for a mail folder.
type Mailbox struct {
	ID          uint64
	UserID      uint64
	Name        string
	UIDValidity uint32
	UIDNext     uint32
}

// Message is the domain model for a stored email.
type Message struct {
	ID           uint64
	MailboxID    uint64
	UID          uint32
	Flags        string
	SizeBytes    int64
	RawKey       string
	EnvelopeFrom string
	EnvelopeTo   string
	Subject      string
}
