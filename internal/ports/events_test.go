package ports

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

// TestMessageReceivedEvent verifies the MessageReceived event implements the Event interface.
func TestMessageReceivedEvent(t *testing.T) {
	now := time.Now()
	event := MessageReceived{
		From:      "alice@gomail.local",
		To:        []string{"bob@gomail.local"},
		Body:      []byte("hello"),
		Size:      5,
		Timestamp: now,
	}

	assert.Equal(t, "message.received", event.EventName())
	assert.Equal(t, now, event.OccurredAt())
}

// TestMessageDeliveredEvent verifies the MessageDelivered event implements the Event interface.
func TestMessageDeliveredEvent(t *testing.T) {
	now := time.Now()
	event := MessageDelivered{
		MessageID: 1,
		MailboxID: 2,
		UID:       3,
		To:        "bob@gomail.local",
		Timestamp: now,
	}

	assert.Equal(t, "message.delivered", event.EventName())
	assert.Equal(t, now, event.OccurredAt())
}
