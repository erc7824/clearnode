package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

// RPCMessage represents a complete message in the RPC protocol, including data and signatures
type RPCMessage struct {
	Data RPCData  `json:"data"`
	Sig  []string `json:"sig"`
}

// RPCData represents the common structure for both requests and responses
// Format: [request_id, type(req/res), method, params, ts]
type RPCData struct {
	RequestID uint64
	Type      string
	Method    string
	Params    []any
	Timestamp uint64
}

// ParseRPCMessage parses a JSON string into a RPCRequest
func ParseRPCMessage(data []byte) (*RPCMessage, error) {
	var req RPCMessage
	if err := json.Unmarshal(data, &req); err != nil {
		return nil, fmt.Errorf("failed to parse request: %w", err)
	}
	return &req, nil
}

// CreateResponse creates a response from a request with the given fields
func CreateResponse(id uint64, method string, responseParams []any, newTimestamp time.Time) *RPCMessage {
	return &RPCMessage{
		Data: RPCData{
			RequestID: id,
			Type:      "res",
			Method:    method,
			Params:    responseParams,
			Timestamp: uint64(newTimestamp.Unix()),
		},
		Sig: []string{},
	}
}

// UnmarshalJSON implements the json.Unmarshaler interface for RPCMessage
func (m *RPCData) UnmarshalJSON(data []byte) error {
	// Parse as raw JSON array first
	var rawMsg []json.RawMessage
	if err := json.Unmarshal(data, &rawMsg); err != nil {
		return err
	}

	// Validate array length
	if len(rawMsg) != 5 {
		return errors.New("invalid message format: expected 4 elements")
	}

	// Parse RequestID (uint64)
	var requestID uint64
	if err := json.Unmarshal(rawMsg[0], &requestID); err != nil {
		return fmt.Errorf("invalid rpc message id: %w", err)
	}
	m.RequestID = uint64(requestID)

	// Parse Type (string)
	if err := json.Unmarshal(rawMsg[1], &m.Type); err != nil {
		return fmt.Errorf("invalid type: %w", err)
	}

	// Parse Method (string)
	if err := json.Unmarshal(rawMsg[2], &m.Method); err != nil {
		return fmt.Errorf("invalid method: %w", err)
	}

	// Parse Params ([]any)
	if err := json.Unmarshal(rawMsg[3], &m.Params); err != nil {
		return fmt.Errorf("invalid params: %w", err)
	}

	// Parse Timestamp (uint64)
	var timestamp uint64
	if err := json.Unmarshal(rawMsg[4], &timestamp); err != nil {
		return fmt.Errorf("invalid timestamp: %w", err)
	}
	m.Timestamp = uint64(timestamp)

	return nil
}

// MarshalJSON implements the json.Marshaler interface for RPCMessage
func (m RPCData) MarshalJSON() ([]byte, error) {
	// Create array representation
	return json.Marshal([]any{
		m.RequestID,
		m.Type,
		m.Method,
		m.Params,
		m.Timestamp,
	})
}
