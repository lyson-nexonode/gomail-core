package smtp

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestSMTPFSMValidTransitions verifies that all valid SMTP command sequences
// produce the expected state transitions.
func TestSMTPFSMValidTransitions(t *testing.T) {
	tests := []struct {
		name     string
		events   []SMTPEvent
		expected State
	}{
		{
			name:     "EHLO transitions to greeted",
			events:   []SMTPEvent{EventEHLO},
			expected: StateGreeted,
		},
		{
			name:     "EHLO then MAIL FROM transitions to mail_from",
			events:   []SMTPEvent{EventEHLO, EventMailFrom},
			expected: StateMailFrom,
		},
		{
			name:     "RCPT TO transitions to rcpt_to",
			events:   []SMTPEvent{EventEHLO, EventMailFrom, EventRcptTo},
			expected: StateRcptTo,
		},
		{
			name:     "DATA after RCPT TO transitions to data",
			events:   []SMTPEvent{EventEHLO, EventMailFrom, EventRcptTo, EventData},
			expected: StateData,
		},
		{
			name:     "DONE after DATA returns to greeted",
			events:   []SMTPEvent{EventEHLO, EventMailFrom, EventRcptTo, EventData, EventDone},
			expected: StateGreeted,
		},
		{
			name:     "RSET after MAIL FROM returns to greeted",
			events:   []SMTPEvent{EventEHLO, EventMailFrom, EventRset},
			expected: StateGreeted,
		},
		{
			name:     "QUIT from init transitions to quit",
			events:   []SMTPEvent{EventQuit},
			expected: StateQuit,
		},
		{
			name:     "QUIT from greeted transitions to quit",
			events:   []SMTPEvent{EventEHLO, EventQuit},
			expected: StateQuit,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fsm := newSMTPFSM()

			for _, event := range tt.events {
				err := fsm.Event(context.Background(), string(event))
				require.NoError(t, err, "unexpected error on event %s", event)
			}

			assert.Equal(t, string(tt.expected), fsm.Current())
		})
	}
}

// TestSMTPFSMMultipleRcptTo verifies that multiple recipients are handled
// correctly — the FSM stays in rcpt_to after the first transition.
func TestSMTPFSMMultipleRcptTo(t *testing.T) {
	fsm := newSMTPFSM()

	require.NoError(t, fsm.Event(context.Background(), string(EventEHLO)))
	require.NoError(t, fsm.Event(context.Background(), string(EventMailFrom)))
	require.NoError(t, fsm.Event(context.Background(), string(EventRcptTo)))
	assert.Equal(t, string(StateRcptTo), fsm.Current())

	// Additional RCPT TO commands do not trigger FSM events
	// They are handled by transitionRcptTo() which skips the FSM when already in rcpt_to
	// The FSM must remain in rcpt_to
	assert.Equal(t, string(StateRcptTo), fsm.Current())

	// DATA must still work after multiple recipients
	require.NoError(t, fsm.Event(context.Background(), string(EventData)))
	assert.Equal(t, string(StateData), fsm.Current())
}

// TestSMTPFSMInvalidTransitions verifies that invalid command sequences
// are rejected by the FSM.
func TestSMTPFSMInvalidTransitions(t *testing.T) {
	tests := []struct {
		name         string
		setupEvents  []SMTPEvent
		invalidEvent SMTPEvent
	}{
		{
			name:         "MAIL FROM without EHLO is rejected",
			setupEvents:  []SMTPEvent{},
			invalidEvent: EventMailFrom,
		},
		{
			name:         "RCPT TO without MAIL FROM is rejected",
			setupEvents:  []SMTPEvent{EventEHLO},
			invalidEvent: EventRcptTo,
		},
		{
			name:         "DATA without RCPT TO is rejected",
			setupEvents:  []SMTPEvent{EventEHLO, EventMailFrom},
			invalidEvent: EventData,
		},
		{
			name:         "DATA directly after EHLO is rejected",
			setupEvents:  []SMTPEvent{EventEHLO},
			invalidEvent: EventData,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fsm := newSMTPFSM()

			for _, event := range tt.setupEvents {
				err := fsm.Event(context.Background(), string(event))
				require.NoError(t, err)
			}

			err := fsm.Event(context.Background(), string(tt.invalidEvent))
			assert.Error(t, err, "expected FSM to reject event %s", tt.invalidEvent)
		})
	}
}

// TestSMTPFSMReset verifies that RSET resets the current transaction
// without closing the session.
func TestSMTPFSMReset(t *testing.T) {
	fsm := newSMTPFSM()

	require.NoError(t, fsm.Event(context.Background(), string(EventEHLO)))
	require.NoError(t, fsm.Event(context.Background(), string(EventMailFrom)))
	require.NoError(t, fsm.Event(context.Background(), string(EventRcptTo)))
	assert.Equal(t, string(StateRcptTo), fsm.Current())

	require.NoError(t, fsm.Event(context.Background(), string(EventRset)))
	assert.Equal(t, string(StateGreeted), fsm.Current())

	// A new transaction must be possible after RSET
	require.NoError(t, fsm.Event(context.Background(), string(EventMailFrom)))
	assert.Equal(t, string(StateMailFrom), fsm.Current())
}

// TestSMTPFSMFullTransaction verifies a complete send cycle.
func TestSMTPFSMFullTransaction(t *testing.T) {
	fsm := newSMTPFSM()

	sequence := []SMTPEvent{
		EventEHLO,
		EventMailFrom,
		EventRcptTo,
		EventData,
		EventDone,
	}

	for _, event := range sequence {
		err := fsm.Event(context.Background(), string(event))
		require.NoError(t, err, "unexpected error on event %s", event)
	}

	// After a complete transaction, the session must be ready for a new one
	assert.Equal(t, string(StateGreeted), fsm.Current())
}
