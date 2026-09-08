// Copyright (c) 2024 OData MCP Contributors
// SPDX-License-Identifier: MIT

package mcp

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/zmcp/odata-mcp/internal/transport"
)

// odataErrorBody is what an OData V3 service returns for an unknown key. The
// quotes in it used to make the error response unmarshalable.
const odataErrorBody = `HTTP 404: {"odata.error":{"code":"","message":{"lang":"en-US","value":"Resource not found for the segment 'People'."}}}`

func TestCreateErrorResponseProducesMarshalableMessage(t *testing.T) {
	tests := []struct {
		name string
		data string
	}{
		{name: "plain text", data: "Tool not found: OData_for_svc"},
		{name: "empty", data: ""},
		{name: "OData JSON error body", data: odataErrorBody},
		{name: "double quotes", data: `he said "no"`},
		{name: "backslashes", data: `C:\path\to\thing`},
		{name: "newlines and tabs", data: "line one\nline two\tindented"},
		{name: "control characters", data: "before\x00\x1fafter"},
		{name: "invalid UTF-8", data: "bad \xff\xfe bytes"},
		{name: "unicode", data: "café ☕ 日本語"},
	}

	server := NewServer("test-server", "1.0.0")

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			msg := server.createErrorResponse(json.RawMessage("3"), -32000, "OData error", tt.data)

			encoded, err := json.Marshal(msg)
			if err != nil {
				t.Fatalf("json.Marshal() error = %v; the client would never receive a response", err)
			}

			var decoded struct {
				JSONRPC string          `json:"jsonrpc"`
				ID      json.RawMessage `json:"id"`
				Error   struct {
					Code    int    `json:"code"`
					Message string `json:"message"`
					Data    string `json:"data"`
				} `json:"error"`
			}
			if err := json.Unmarshal(encoded, &decoded); err != nil {
				t.Fatalf("json.Unmarshal() error = %v, payload = %s", err, encoded)
			}

			if decoded.JSONRPC != "2.0" {
				t.Errorf("jsonrpc = %q, want %q", decoded.JSONRPC, "2.0")
			}
			if string(decoded.ID) != "3" {
				t.Errorf("id = %s, want 3", decoded.ID)
			}
			if decoded.Error.Code != -32000 {
				t.Errorf("error.code = %d, want -32000", decoded.Error.Code)
			}
			if decoded.Error.Message != "OData error" {
				t.Errorf("error.message = %q, want %q", decoded.Error.Message, "OData error")
			}
		})
	}
}

func TestCreateErrorResponseRoundTripsDataVerbatim(t *testing.T) {
	server := NewServer("test-server", "1.0.0")

	msg := server.createErrorResponse(json.RawMessage("7"), -32000, "OData error", odataErrorBody)

	encoded, err := json.Marshal(msg)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}

	var decoded struct {
		Error struct {
			Data string `json:"data"`
		} `json:"error"`
	}
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}

	if decoded.Error.Data != odataErrorBody {
		t.Errorf("error.data = %q, want it preserved as %q", decoded.Error.Data, odataErrorBody)
	}
	if !strings.Contains(decoded.Error.Data, "Resource not found") {
		t.Error("error.data lost the service's own message, which is what a caller needs")
	}
}

func TestCreateErrorResponseNormalizesID(t *testing.T) {
	tests := []struct {
		name string
		id   interface{}
		want string
	}{
		{name: "raw number", id: json.RawMessage("42"), want: "42"},
		{name: "raw string", id: json.RawMessage(`"abc"`), want: `"abc"`},
		{name: "raw null becomes zero", id: json.RawMessage("null"), want: "0"},
		{name: "empty raw becomes zero", id: json.RawMessage(""), want: "0"},
		{name: "nil becomes zero", id: nil, want: "0"},
		{name: "plain int", id: 5, want: "5"},
	}

	server := NewServer("test-server", "1.0.0")

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			msg := server.createErrorResponse(tt.id, -32600, "Invalid Request", "")

			if got := string(msg.ID); got != tt.want {
				t.Errorf("ID = %s, want %s", got, tt.want)
			}
			if _, err := json.Marshal(msg); err != nil {
				t.Errorf("json.Marshal() error = %v", err)
			}
		})
	}
}

// A message the transport cannot marshal is never written, which the caller
// sees as a hang rather than an error.
func TestErrorResponseIsWritableByTransport(t *testing.T) {
	server := NewServer("test-server", "1.0.0")

	msg := server.createErrorResponse(json.RawMessage("1"), -32000, "OData error", odataErrorBody)

	var written transport.Message
	encoded, err := json.Marshal(msg)
	if err != nil {
		t.Fatalf("transport would fail to marshal the message: %v", err)
	}
	if err := json.Unmarshal(encoded, &written); err != nil {
		t.Fatalf("a client could not decode the message: %v", err)
	}
	if written.Error == nil {
		t.Fatal("decoded message carries no error object")
	}
	if written.Error.Code != -32000 {
		t.Errorf("decoded error code = %d, want -32000", written.Error.Code)
	}
}
