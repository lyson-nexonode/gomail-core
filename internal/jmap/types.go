package jmap

// Capabilities advertised in the JMAP session endpoint.
// These URNs tell the client which JMAP methods are available.
const (
	CapabilityCore = "urn:ietf:params:jmap:core"
	CapabilityMail = "urn:ietf:params:jmap:mail"
)

// Request represents a JMAP API request body (RFC 8620 section 3.3).
type Request struct {
	Using       []string     `json:"using"`
	MethodCalls []MethodCall `json:"methodCalls"`
}

// MethodCall is a single method invocation within a JMAP request.
// It is a 3-element tuple: [method_name, arguments, call_id].
type MethodCall struct {
	Name   string
	Args   map[string]interface{}
	CallID string
}

// Response represents a JMAP API response body (RFC 8620 section 3.4).
type Response struct {
	MethodResponses []MethodResponse `json:"methodResponses"`
	SessionState    string           `json:"sessionState"`
}

// MethodResponse is a single method result within a JMAP response.
// It mirrors the MethodCall structure: [method_name, result, call_id].
type MethodResponse struct {
	Name   string
	Result interface{}
	CallID string
}

// SessionResponse is returned by the JMAP session endpoint (RFC 8620 section 2).
// It describes the server capabilities and the user's accounts.
type SessionResponse struct {
	Capabilities map[string]interface{} `json:"capabilities"`
	Accounts     map[string]Account     `json:"accounts"`
	PrimaryAccounts map[string]string   `json:"primaryAccounts"`
	Username     string                 `json:"username"`
	APIURL       string                 `json:"apiUrl"`
	State        string                 `json:"state"`
}

// Account represents a JMAP account (RFC 8620 section 1.6.2).
type Account struct {
	Name         string                 `json:"name"`
	IsPersonal   bool                   `json:"isPersonal"`
	Capabilities map[string]interface{} `json:"accountCapabilities"`
}

// Error represents a JMAP method error response (RFC 8620 section 3.6.1).
type Error struct {
	Type        string `json:"type"`
	Description string `json:"description,omitempty"`
}

// JMAPMailbox represents a mailbox in JMAP (RFC 8621 section 2).
type JMAPMailbox struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	Role         string `json:"role,omitempty"`
	TotalEmails  int    `json:"totalEmails"`
	UnreadEmails int    `json:"unreadEmails"`
}

// JMAPEmail represents an email in JMAP (RFC 8621 section 4).
type JMAPEmail struct {
	ID           string   `json:"id"`
	MailboxIDs   map[string]bool `json:"mailboxIds"`
	Subject      string   `json:"subject"`
	From         []Address `json:"from,omitempty"`
	To           []Address `json:"to,omitempty"`
	ReceivedAt   string   `json:"receivedAt"`
	Size         int64    `json:"size"`
	BodyValues   map[string]BodyValue `json:"bodyValues,omitempty"`
}

// Address represents an email address in JMAP format.
type Address struct {
	Email string `json:"email"`
	Name  string `json:"name,omitempty"`
}

// BodyValue represents the body content of an email part.
type BodyValue struct {
	Value string `json:"value"`
}
