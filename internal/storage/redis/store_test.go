package redis

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestMessageBodyKey verifies that Redis keys are correctly formatted.
func TestMessageBodyKey(t *testing.T) {
	tests := []struct {
		messageID uint64
		expected  string
	}{
		{1, "msg:body:1"},
		{42, "msg:body:42"},
		{99999, "msg:body:99999"},
	}

	for _, tt := range tests {
		assert.Equal(t, tt.expected, messageBodyKey(tt.messageID))
	}
}
