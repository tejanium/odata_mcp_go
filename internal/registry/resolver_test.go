// Copyright (c) 2024 OData MCP Contributors
// SPDX-License-Identifier: MIT

package registry

import (
	"net/http"
	"testing"
)

func defaultResolver() Resolver {
	return Resolver{
		Defaults: Credentials{
			ServiceURL:   "https://tenant.example.com/odata/service.svc/",
			ClientID:     "configured-id",
			ClientSecret: "configured-secret",
			TokenURL:     "https://tenant.example.com/OAuth/Token",
			Scope:        "read",
		},
		AllowedServiceURLs: []string{"https://tenant.example.com/odata/"},
	}
}

func TestResolveFallsBackToDefaults(t *testing.T) {
	creds, err := defaultResolver().Resolve(http.Header{})
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}

	if creds.ClientID != "configured-id" || creds.ServiceURL != "https://tenant.example.com/odata/service.svc/" {
		t.Errorf("Resolve() = %+v, want the configured defaults", creds.Redacted())
	}
}

func TestResolveTakesCredentialsFromHeaders(t *testing.T) {
	headers := http.Header{}
	headers.Set(HeaderClientID, "caller-id")
	headers.Set(HeaderClientSecret, "caller-secret")
	headers.Set(HeaderScope, "write")

	creds, err := defaultResolver().Resolve(headers)
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}

	if creds.ClientID != "caller-id" || creds.ClientSecret != "caller-secret" || creds.Scope != "write" {
		t.Errorf("Resolve() = %+v, want the header values", creds.Redacted())
	}
	if creds.TokenURL != "https://tenant.example.com/OAuth/Token" {
		t.Errorf("TokenURL = %q, want the default to survive", creds.TokenURL)
	}
}

func TestResolveTreatsABearerAsTheWholeCredential(t *testing.T) {
	headers := http.Header{}
	headers.Set("Authorization", "Bearer pasted-token")

	creds, err := defaultResolver().Resolve(headers)
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}

	if creds.BearerToken != "pasted-token" {
		t.Errorf("BearerToken = %q, want the header token", creds.BearerToken)
	}
	if creds.ClientID != "" || creds.ClientSecret != "" {
		t.Errorf("Resolve() = %+v, want the configured client credentials dropped", creds.Redacted())
	}
}

func TestResolveRejectsAServiceURLTheOperatorDidNotAllow(t *testing.T) {
	tests := []struct {
		name      string
		requested string
		allowed   bool
	}{
		{"exactly the allowed prefix", "https://tenant.example.com/odata/", true},
		{"below the allowed prefix", "https://tenant.example.com/odata/other.svc/", true},
		{"a different host", "https://evil.test/odata/", false},
		{"a different scheme", "http://tenant.example.com/odata/", false},
		{"outside the allowed path", "https://tenant.example.com/internal/", false},
		{"userinfo spoofing the host", "https://tenant.example.com@evil.test/odata/", false},
		{"host as a string prefix only", "https://tenant.example.com.evil.test/odata/", false},
		{"not a url", "not-a-url", false},
		{"a non-http scheme", "file:///etc/passwd", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			headers := http.Header{}
			headers.Set(HeaderServiceURL, tt.requested)

			creds, err := defaultResolver().Resolve(headers)

			if tt.allowed {
				if err != nil {
					t.Fatalf("Resolve() error = %v, want %q accepted", err, tt.requested)
				}
				if creds.ServiceURL != tt.requested {
					t.Errorf("ServiceURL = %q, want %q", creds.ServiceURL, tt.requested)
				}
				return
			}

			if err == nil {
				t.Errorf("Resolve() accepted %q, want it refused", tt.requested)
			}
		})
	}
}

func TestResolveIgnoresTheServiceURLHeaderWhenNothingIsAllowed(t *testing.T) {
	resolver := defaultResolver()
	resolver.AllowedServiceURLs = nil

	headers := http.Header{}
	headers.Set(HeaderServiceURL, "https://tenant.example.com/odata/service.svc/")

	if _, err := resolver.Resolve(headers); err == nil {
		t.Error("Resolve() honoured a service URL header with no allow list configured")
	}
}

func TestResolveReportsMissingCredentials(t *testing.T) {
	resolver := Resolver{Defaults: Credentials{ServiceURL: "https://tenant.example.com/odata/"}}

	if _, err := resolver.Resolve(http.Header{}); err == nil {
		t.Error("Resolve() returned no error for a request carrying no credential")
	}
}

func TestParseBearer(t *testing.T) {
	tests := []struct {
		header string
		want   string
	}{
		{"Bearer token", "token"},
		{"bearer token", "token"},
		{"BEARER token", "token"},
		{"Bearer  padded  ", "padded"},
		{"Basic token", ""},
		{"Bearer", ""},
		{"Bearer ", ""},
		{"token", ""},
		{"", ""},
	}

	for _, tt := range tests {
		t.Run(tt.header, func(t *testing.T) {
			if got := parseBearer(tt.header); got != tt.want {
				t.Errorf("parseBearer(%q) = %q, want %q", tt.header, got, tt.want)
			}
		})
	}
}
