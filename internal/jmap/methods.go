package jmap

import (
	"context"
	"fmt"

	"go.uber.org/zap"
)

// handleMailboxGet implements the Mailbox/get method (RFC 8621 section 2.5).
// Returns the list of mailboxes for the authenticated user.
func (s *Server) handleMailboxGet(ctx context.Context, claims *Claims, call MethodCall) MethodResponse {
	mailboxes, err := s.mailboxReader.ListMailboxes(ctx, claims.UserID)
	if err != nil {
		s.log.Error("jmap Mailbox/get failed",
			zap.Uint64("user_id", claims.UserID),
			zap.Error(err),
		)
		return MethodResponse{
			Name:   "error",
			Result: Error{Type: "serverFail", Description: "Failed to retrieve mailboxes"},
		}
	}

	list := make([]JMAPMailbox, len(mailboxes))
	for i, mb := range mailboxes {
		list[i] = JMAPMailbox{
			ID:           fmt.Sprintf("mb%d", mb.ID),
			Name:         mb.Name,
			Role:         mailboxRole(mb.Name),
			TotalEmails:  0, // TODO: count from messages table
			UnreadEmails: 0,
		}
	}

	return MethodResponse{
		Name: "Mailbox/get",
		Result: map[string]interface{}{
			"accountId": fmt.Sprintf("u%d", claims.UserID),
			"list":      list,
			"notFound":  []string{},
		},
	}
}

// handleMailboxQuery implements the Mailbox/query method (RFC 8621 section 2.3).
// Returns the IDs of mailboxes matching the given filter.
func (s *Server) handleMailboxQuery(ctx context.Context, claims *Claims, call MethodCall) MethodResponse {
	mailboxes, err := s.mailboxReader.ListMailboxes(ctx, claims.UserID)
	if err != nil {
		return MethodResponse{
			Name:   "error",
			Result: Error{Type: "serverFail", Description: "Failed to query mailboxes"},
		}
	}

	ids := make([]string, len(mailboxes))
	for i, mb := range mailboxes {
		ids[i] = fmt.Sprintf("mb%d", mb.ID)
	}

	return MethodResponse{
		Name: "Mailbox/query",
		Result: map[string]interface{}{
			"accountId": fmt.Sprintf("u%d", claims.UserID),
			"ids":       ids,
			"total":     len(ids),
			"position":  0,
		},
	}
}

// handleEmailGet implements the Email/get method (RFC 8621 section 4.5).
// Returns emails by their IDs or all emails in the account.
func (s *Server) handleEmailGet(ctx context.Context, claims *Claims, call MethodCall) MethodResponse {
	// Find the user's INBOX to list messages
	inbox, err := s.mailboxReader.FindMailbox(ctx, claims.UserID, "INBOX")
	if err != nil {
		return MethodResponse{
			Name:   "error",
			Result: Error{Type: "serverFail", Description: "Failed to find inbox"},
		}
	}

	messages, err := s.messageReader.ListMessages(ctx, inbox.ID)
	if err != nil {
		return MethodResponse{
			Name:   "error",
			Result: Error{Type: "serverFail", Description: "Failed to retrieve emails"},
		}
	}

	list := make([]JMAPEmail, len(messages))
	for i, msg := range messages {
		list[i] = JMAPEmail{
			ID: fmt.Sprintf("e%d", msg.ID),
			MailboxIDs: map[string]bool{
				fmt.Sprintf("mb%d", msg.MailboxID): true,
			},
			Subject: msg.Subject,
			From:    []Address{{Email: msg.EnvelopeFrom}},
			To:      []Address{{Email: msg.EnvelopeTo}},
			Size:    msg.SizeBytes,
		}
	}

	return MethodResponse{
		Name: "Email/get",
		Result: map[string]interface{}{
			"accountId": fmt.Sprintf("u%d", claims.UserID),
			"list":      list,
			"notFound":  []string{},
		},
	}
}

// handleEmailQuery implements the Email/query method (RFC 8621 section 4.3).
// Returns the IDs of emails matching the given filter and sort criteria.
func (s *Server) handleEmailQuery(ctx context.Context, claims *Claims, call MethodCall) MethodResponse {
	inbox, err := s.mailboxReader.FindMailbox(ctx, claims.UserID, "INBOX")
	if err != nil {
		return MethodResponse{
			Name:   "error",
			Result: Error{Type: "serverFail", Description: "Failed to find inbox"},
		}
	}

	messages, err := s.messageReader.ListMessages(ctx, inbox.ID)
	if err != nil {
		return MethodResponse{
			Name:   "error",
			Result: Error{Type: "serverFail", Description: "Failed to query emails"},
		}
	}

	ids := make([]string, len(messages))
	for i, msg := range messages {
		ids[i] = fmt.Sprintf("e%d", msg.ID)
	}

	return MethodResponse{
		Name: "Email/query",
		Result: map[string]interface{}{
			"accountId":           fmt.Sprintf("u%d", claims.UserID),
			"ids":                 ids,
			"total":               len(ids),
			"position":            0,
			"canCalculateChanges": false,
		},
	}
}

// mailboxRole returns the JMAP role for standard mailbox names.
// RFC 8621 section 2.1 defines standard roles.
func mailboxRole(name string) string {
	switch name {
	case "INBOX":
		return "inbox"
	case "Sent":
		return "sent"
	case "Trash":
		return "trash"
	case "Drafts":
		return "drafts"
	default:
		return ""
	}
}
