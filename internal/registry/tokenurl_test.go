// Copyright (c) 2024 OData MCP Contributors
// SPDX-License-Identifier: MIT

package registry

import (
	"net/http"
	"testing"
)

func clientCredentialHeaders() http.Header {
	headers := http.Header{}
	headers.Set(HeaderClientID, "caller-id")
	headers.Set(HeaderClientSecret, "caller-secret")

	return headers
}

func TestResolveKeepsTheTokenURLOnAnAllowedHost(t *testing.T) {
	tests := []struct {
		name      string
		requested string
		allowed   bool
	}{
		{"beside the service, different path and case", "https://tenant.example.com/OAuth/Token", true},
		{"a different host", "https://evil.test/OAuth/Token", false},
		{"a loopback address", "http://127.0.0.1/latest/meta-data/", false},
		{"a different scheme", "http://tenant.example.com/OAuth/Token", false},
		{"userinfo spoofing the host", "https://tenant.example.com@evil.test/OAuth/Token", false},
		{"host as a string prefix only", "https://tenant.example.com.evil.test/OAuth/Token", false},
		{"a different port", "https://tenant.example.com:8443/OAuth/Token", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			headers := clientCredentialHeaders()
			headers.Set(HeaderTokenURL, tt.requested)

			creds, err := defaultResolver().Resolve(headers)

			if tt.allowed {
				if err != nil {
					t.Fatalf("Resolve() error = %v, want %q accepted", err, tt.requested)
				}
				if creds.TokenURL != tt.requested {
					t.Errorf("TokenURL = %q, want %q", creds.TokenURL, tt.requested)
				}
				return
			}

			if err == nil {
				t.Errorf("Resolve() accepted token URL %q, want it refused", tt.requested)
			}
		})
	}
}

func TestResolveRefusesATokenURLHeaderWhenNothingIsAllowed(t *testing.T) {
	resolver := defaultResolver()
	resolver.AllowedServiceURLs = nil

	headers := clientCredentialHeaders()
	headers.Set(HeaderTokenURL, "https://tenant.example.com/OAuth/Token")

	if _, err := resolver.Resolve(headers); err == nil {
		t.Error("Resolve() honoured a token URL header with no allow list configured")
	}
}

func TestResolveStillUsesTheConfiguredTokenURLWithoutAHeader(t *testing.T) {
	creds, err := defaultResolver().Resolve(clientCredentialHeaders())
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}

	if creds.TokenURL != "https://tenant.example.com/OAuth/Token" {
		t.Errorf("TokenURL = %q, want the operator's default", creds.TokenURL)
	}
}
