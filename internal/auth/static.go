// Copyright (c) 2024 OData MCP Contributors
// SPDX-License-Identifier: MIT

package auth

import (
	"context"
	"fmt"
)

// StaticTokenSource presents a token the caller already holds, for services
// where an administrator mints one and pastes it into client configuration.
type StaticTokenSource struct {
	header string
}

// NewStaticTokenSource returns a TokenSource for a fixed bearer token.
func NewStaticTokenSource(token string) (*StaticTokenSource, error) {
	if token == "" {
		return nil, fmt.Errorf("auth: bearer token is required")
	}

	return &StaticTokenSource{header: bearerTokenType + " " + token}, nil
}

// AuthorizationHeader returns the fixed credential.
func (s *StaticTokenSource) AuthorizationHeader(context.Context) (string, error) {
	return s.header, nil
}
