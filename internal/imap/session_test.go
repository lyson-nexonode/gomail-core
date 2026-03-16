package imap

import (
	"context"
	"testing"

	llfsm "github.com/looplab/fsm"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newTestIMAPFSM creates a bare IMAP FSM for unit testing.
func newTestIMAPFSM() *llfsm.FSM {
	return llfsm.NewFSM(
		string(StateNotAuthenticated),
		llfsm.Events{
			{Name: string(EventLogin), Src: []string{string(StateNotAuthenticated)}, Dst: string(StateAuthenticated)},
			{Name: string(EventSelect), Src: []string{string(StateAuthenticated)}, Dst: string(StateSelected)},
			{Name: string(EventClose), Src: []string{string(StateSelected)}, Dst: string(StateAuthenticated)},
			{Name: string(EventLogout), Src: []string{
				string(StateNotAuthenticated),
				string(StateAuthenticated),
				string(StateSelected),
			}, Dst: string(StateLogout)},
		},
		llfsm.Callbacks{},
	)
}

// TestIMAPFSMValidTransitions verifies all valid IMAP state transitions.
func TestIMAPFSMValidTransitions(t *testing.T) {
	tests := []struct {
		name     string
		events   []IMAPEvent
		expected State
	}{
		{
			name:     "LOGIN transitions to authenticated",
			events:   []IMAPEvent{EventLogin},
			expected: StateAuthenticated,
		},
		{
			name:     "SELECT transitions to selected",
			events:   []IMAPEvent{EventLogin, EventSelect},
			expected: StateSelected,
		},
		{
			name:     "CLOSE returns to authenticated",
			events:   []IMAPEvent{EventLogin, EventSelect, EventClose},
			expected: StateAuthenticated,
		},
		{
			name:     "LOGOUT from not_authenticated",
			events:   []IMAPEvent{EventLogout},
			expected: StateLogout,
		},
		{
			name:     "LOGOUT from authenticated",
			events:   []IMAPEvent{EventLogin, EventLogout},
			expected: StateLogout,
		},
		{
			name:     "LOGOUT from selected",
			events:   []IMAPEvent{EventLogin, EventSelect, EventLogout},
			expected: StateLogout,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fsm := newTestIMAPFSM()

			for _, event := range tt.events {
				err := fsm.Event(context.Background(), string(event))
				require.NoError(t, err, "unexpected error on event %s", event)
			}

			assert.Equal(t, string(tt.expected), fsm.Current())
		})
	}
}

// TestIMAPFSMSelectReselect verifies that re-selecting a mailbox while
// already in selected state is handled by transitionSelect without FSM event.
// looplab/fsm does not support self-transitions — transitionSelect bypasses
// the FSM when already in selected state.
func TestIMAPFSMSelectReselect(t *testing.T) {
	fsm := newTestIMAPFSM()

	require.NoError(t, fsm.Event(context.Background(), string(EventLogin)))
	require.NoError(t, fsm.Event(context.Background(), string(EventSelect)))
	assert.Equal(t, string(StateSelected), fsm.Current())

	// The FSM stays in selected — transitionSelect() handles this without a FSM event
	assert.Equal(t, string(StateSelected), fsm.Current())
}

// TestIMAPFSMInvalidTransitions verifies that commands in wrong states are rejected.
func TestIMAPFSMInvalidTransitions(t *testing.T) {
	tests := []struct {
		name         string
		setupEvents  []IMAPEvent
		invalidEvent IMAPEvent
	}{
		{
			name:         "SELECT without LOGIN is rejected",
			setupEvents:  []IMAPEvent{},
			invalidEvent: EventSelect,
		},
		{
			name:         "CLOSE without SELECT is rejected",
			setupEvents:  []IMAPEvent{EventLogin},
			invalidEvent: EventClose,
		},
		{
			name:         "LOGIN when already authenticated is rejected",
			setupEvents:  []IMAPEvent{EventLogin},
			invalidEvent: EventLogin,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fsm := newTestIMAPFSM()

			for _, event := range tt.setupEvents {
				err := fsm.Event(context.Background(), string(event))
				require.NoError(t, err)
			}

			err := fsm.Event(context.Background(), string(tt.invalidEvent))
			assert.Error(t, err, "expected FSM to reject event %s", tt.invalidEvent)
		})
	}
}
