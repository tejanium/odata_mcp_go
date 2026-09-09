// Copyright (c) 2024 OData MCP Contributors
// SPDX-License-Identifier: MIT

package auth

import (
	"context"
	"strings"
	"testing"
)

func TestNewStaticTokenSourceRequiresAToken(t *testing.T) {
	if _, err := NewStaticTokenSource(""); err == nil {
		t.Error("NewStaticTokenSource(\"\") returned no error")
	}
}

func TestStaticTokenSourcePresentsTheTokenAsBearer(t *testing.T) {
	source, err := NewStaticTokenSource("pasted-token")
	if err != nil {
		t.Fatalf("NewStaticTokenSource() error = %v", err)
	}

	header, err := source.AuthorizationHeader(context.Background())
	if err != nil {
		t.Fatalf("AuthorizationHeader() error = %v", err)
	}

	if header != "Bearer pasted-token" {
		t.Errorf("AuthorizationHeader() = %q, want %q", header, "Bearer pasted-token")
	}
}

func TestStaticTokenSourceIsStableAcrossCalls(t *testing.T) {
	source, err := NewStaticTokenSource("pasted-token")
	if err != nil {
		t.Fatalf("NewStaticTokenSource() error = %v", err)
	}

	ctx := context.Background()
	first, _ := source.AuthorizationHeader(ctx)

	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	second, err := source.AuthorizationHeader(cancelled)

	if err != nil {
		t.Fatalf("AuthorizationHeader() with a cancelled context error = %v, want none for a static token", err)
	}
	if first != second {
		t.Errorf("AuthorizationHeader() = %q then %q, want it stable", first, second)
	}
	if !strings.HasPrefix(second, "Bearer ") {
		t.Errorf("AuthorizationHeader() = %q, want a Bearer credential", second)
	}
}
