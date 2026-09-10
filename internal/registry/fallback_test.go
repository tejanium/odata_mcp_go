// Copyright (c) 2024 OData MCP Contributors
// SPDX-License-Identifier: MIT

package registry

import (
	"net/http"
	"testing"
)

func strictResolver() Resolver {
	return Resolver{
		Defaults: Credentials{
			ServiceURL:   "https://tenant.example.com/odata/service.svc/",
			ClientID:     "configured-id",
			ClientSecret: "configured-secret",
			TokenURL:     "https://tenant.example.com/OAuth/Token",
			Scope:        "read",
		},
		AllowedServiceURLs:        []string{"https://tenant.example.com/odata/"},
		RequireRequestCredentials: true,
	}
}

func TestStrictResolverRefusesACredentiallessRequest(t *testing.T) {
	if _, err := strictResolver().Resolve(http.Header{}); err == nil {
		t.Fatal("a request with no credential inherited the configured one")
	}
}

func TestStrictResolverRefusesAServiceURLWithoutACredential(t *testing.T) {
	headers := http.Header{}
	headers.Set(HeaderServiceURL, "https://tenant.example.com/odata/service.svc/")

	if _, err := strictResolver().Resolve(headers); err == nil {
		t.Fatal("naming a service was enough to borrow the configured credential")
	}
}

func TestStrictResolverAcceptsACallerSuppliedSecret(t *testing.T) {
	headers := http.Header{}
	headers.Set(HeaderClientID, "caller-id")
	headers.Set(HeaderClientSecret, "caller-secret")

	creds, err := strictResolver().Resolve(headers)
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}

	if creds.ClientSecret != "caller-secret" || creds.ClientID != "caller-id" {
		t.Errorf("Resolve() = %+v, want the caller's credential", creds.Redacted())
	}
	if creds.TokenURL != "https://tenant.example.com/OAuth/Token" || creds.ServiceURL == "" {
		t.Errorf("Resolve() = %+v, want non-secret defaults to survive", creds.Redacted())
	}
}

func TestStrictResolverAcceptsACallerSuppliedBearer(t *testing.T) {
	headers := http.Header{}
	headers.Set("Authorization", "Bearer caller-token")

	creds, err := strictResolver().Resolve(headers)
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}

	if creds.BearerToken != "caller-token" {
		t.Errorf("BearerToken = %q, want the caller's token", creds.BearerToken)
	}
	if creds.ClientSecret != "" {
		t.Error("the configured client secret survived alongside a caller bearer token")
	}
}

func TestStrictResolverStillRefusesADisallowedService(t *testing.T) {
	headers := http.Header{}
	headers.Set(HeaderServiceURL, "https://evil.test/odata/")
	headers.Set(HeaderClientID, "caller-id")
	headers.Set(HeaderClientSecret, "caller-secret")

	if _, err := strictResolver().Resolve(headers); err == nil {
		t.Fatal("a caller with a valid credential reached a service outside the allow list")
	}
}

func TestNonStrictResolverKeepsInheritingDefaults(t *testing.T) {
	resolver := strictResolver()
	resolver.RequireRequestCredentials = false

	creds, err := resolver.Resolve(http.Header{})
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}

	if creds.ClientSecret != "configured-secret" {
		t.Errorf("ClientSecret = %q, want the single-tenant behaviour unchanged", creds.Redacted().ClientSecret)
	}
}
