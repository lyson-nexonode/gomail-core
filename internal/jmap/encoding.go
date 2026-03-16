package jmap

import (
	"encoding/json"
	"fmt"
)

// UnmarshalJSON implements custom unmarshaling for MethodCall.
// A JMAP method call is a JSON array: ["method/name", {args}, "callId"].
func (m *MethodCall) UnmarshalJSON(data []byte) error {
	var raw []json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return fmt.Errorf("method call must be a JSON array: %w", err)
	}
	if len(raw) != 3 {
		return fmt.Errorf("method call must have exactly 3 elements, got %d", len(raw))
	}

	if err := json.Unmarshal(raw[0], &m.Name); err != nil {
		return fmt.Errorf("method call name: %w", err)
	}

	if err := json.Unmarshal(raw[1], &m.Args); err != nil {
		return fmt.Errorf("method call args: %w", err)
	}

	if err := json.Unmarshal(raw[2], &m.CallID); err != nil {
		return fmt.Errorf("method call id: %w", err)
	}

	return nil
}

// MarshalJSON implements custom marshaling for MethodResponse.
// A JMAP method response is a JSON array: ["method/name", {result}, "callId"].
func (m MethodResponse) MarshalJSON() ([]byte, error) {
	return json.Marshal([]interface{}{m.Name, m.Result, m.CallID})
}
