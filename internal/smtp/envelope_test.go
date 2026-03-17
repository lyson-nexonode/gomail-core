package smtp

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestEnvelopeAddRecipient verifies that recipients are correctly appended.
func TestEnvelopeAddRecipient(t *testing.T) {
	e := &Envelope{}

	assert.False(t, e.HasRecipients())

	e.AddRecipient("alice@gomail.local")
	assert.True(t, e.HasRecipients())
	assert.Len(t, e.To, 1)

	e.AddRecipient("bob@gomail.local")
	assert.Len(t, e.To, 2)
	assert.Equal(t, "alice@gomail.local", e.To[0])
	assert.Equal(t, "bob@gomail.local", e.To[1])
}

// TestEnvelopeReset verifies that Reset clears all fields.
func TestEnvelopeReset(t *testing.T) {
	e := &Envelope{
		From: "alice@gomail.local",
		To:   []string{"bob@gomail.local"},
		Data: []byte("hello"),
		Size: 5,
	}

	e.Reset()

	assert.Empty(t, e.From)
	assert.Empty(t, e.To)
	assert.Empty(t, e.Data)
	assert.Equal(t, int64(0), e.Size)
	assert.False(t, e.HasRecipients())
}

// TestEnvelopeHasRecipients verifies the HasRecipients helper.
func TestEnvelopeHasRecipients(t *testing.T) {
	e := &Envelope{}
	assert.False(t, e.HasRecipients())

	e.To = []string{"someone@example.com"}
	assert.True(t, e.HasRecipients())
}
