package imap

import "time"

// Capability lists the IMAP extensions supported by this server.
// Advertised in response to the CAPABILITY command and in the greeting.
var Capability = []string{
	"IMAP4rev1",
	"LITERAL+",
	"SASL-IR",
	"LOGIN",
	"IDLE",
}

// FetchItem represents a data item requested in a FETCH command.
// Examples: FLAGS, ENVELOPE, BODY[], RFC822.SIZE
type FetchItem string

const (
	FetchFlags        FetchItem = "FLAGS"
	FetchEnvelope     FetchItem = "ENVELOPE"
	FetchBodySection  FetchItem = "BODY[]"
	FetchRFC822Size   FetchItem = "RFC822.SIZE"
	FetchInternalDate FetchItem = "INTERNALDATE"
	FetchUID          FetchItem = "UID"
)

// Flag represents an IMAP message flag as defined in RFC 3501 section 2.3.2.
type Flag string

const (
	FlagSeen     Flag = "\\Seen"
	FlagAnswered Flag = "\\Answered"
	FlagFlagged  Flag = "\\Flagged"
	FlagDeleted  Flag = "\\Deleted"
	FlagDraft    Flag = "\\Draft"
	FlagRecent   Flag = "\\Recent"
)

// SelectedMailbox holds the state of the currently selected mailbox.
// This state is maintained per session in the Selected FSM state.
type SelectedMailbox struct {
	ID          uint64
	Name        string
	UIDValidity uint32
	UIDNext     uint32
	ReadOnly    bool // true when opened via EXAMINE instead of SELECT
	SelectedAt  time.Time
}
