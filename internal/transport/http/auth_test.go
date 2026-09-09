// Copyright (c) 2024 OData MCP Contributors
// SPDX-License-Identifier: MIT

package http

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/zmcp/odata-mcp/internal/client"
	"github.com/zmcp/odata-mcp/internal/transport"
)

const gateToken = "s3cret-gate-token"

// reachedHandler answers 200 and records whether it ran at all.
func reachedHandler(reached *bool) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		*reached = true
		w.WriteHeader(http.StatusOK)
	})
}

func TestSecurityMiddlewareGatesOnBearerToken(t *testing.T) {
	tests := []struct {
		name       string
		authHeader string
		wantStatus int
	}{
		{"correct token", "Bearer " + gateToken, http.StatusOK},
		{"scheme is case insensitive", "bearer " + gateToken, http.StatusOK},
		{"no header at all", "", http.StatusUnauthorized},
		{"wrong token", "Bearer not-the-token", http.StatusUnauthorized},
		{"token with a suffix", "Bearer " + gateToken + "x", http.StatusUnauthorized},
		{"correct token as a prefix", "Bearer " + gateToken[:8], http.StatusUnauthorized},
		{"empty credential", "Bearer ", http.StatusUnauthorized},
		{"scheme only", "Bearer", http.StatusUnauthorized},
		{"wrong scheme", "Basic " + gateToken, http.StatusUnauthorized},
		{"bare token without a scheme", gateToken, http.StatusUnauthorized},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader("{}"))
			if tt.authHeader != "" {
				req.Header.Set("Authorization", tt.authHeader)
			}

			reached := false
			rec := httptest.NewRecorder()
			SecurityMiddleware(SecurityConfig{Token: gateToken}, reachedHandler(&reached)).ServeHTTP(rec, req)

			if rec.Code != tt.wantStatus {
				t.Errorf("status = %d, want %d", rec.Code, tt.wantStatus)
			}

			if tt.wantStatus == http.StatusUnauthorized {
				if reached {
					t.Error("handler ran despite the request being rejected")
				}
				if got := rec.Header().Get("WWW-Authenticate"); !strings.HasPrefix(got, "Bearer") {
					t.Errorf("WWW-Authenticate = %q, want a Bearer challenge", got)
				}
			}
		})
	}
}

func TestSecurityMiddlewareLeavesHealthOpen(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, HealthPath, nil)

	reached := false
	rec := httptest.NewRecorder()
	SecurityMiddleware(SecurityConfig{Token: gateToken}, reachedHandler(&reached)).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK || !reached {
		t.Errorf("health: status = %d reached = %v, want 200 and reached", rec.Code, reached)
	}
}

func TestSecurityMiddlewareAnswersPreflightWithoutTheHandler(t *testing.T) {
	req := httptest.NewRequest(http.MethodOptions, "/mcp", nil)
	req.Host = "127.0.0.1:8080"

	reached := false
	rec := httptest.NewRecorder()
	SecurityMiddleware(SecurityConfig{Token: gateToken}, reachedHandler(&reached)).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200 for preflight", rec.Code)
	}
	if reached {
		t.Error("preflight reached the handler")
	}
	if got := rec.Header().Get("Access-Control-Allow-Headers"); !strings.Contains(got, "Authorization") {
		t.Errorf("Access-Control-Allow-Headers = %q, want Authorization to be allowed", got)
	}
}

func TestSecurityMiddlewareWithoutTokenStaysLoopbackOnly(t *testing.T) {
	remote := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader("{}"))

	reached := false
	rec := httptest.NewRecorder()
	SecurityMiddleware(SecurityConfig{}, reachedHandler(&reached)).ServeHTTP(rec, remote)

	if rec.Code != http.StatusForbidden || reached {
		t.Errorf("remote: status = %d reached = %v, want 403 and not reached", rec.Code, reached)
	}

	local := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader("{}"))
	local.RemoteAddr = "127.0.0.1:54321"
	local.Host = "127.0.0.1:8080"

	reached = false
	rec = httptest.NewRecorder()
	SecurityMiddleware(SecurityConfig{}, reachedHandler(&reached)).ServeHTTP(rec, local)

	if rec.Code != http.StatusOK || !reached {
		t.Errorf("loopback: status = %d reached = %v, want 200 and reached", rec.Code, reached)
	}
}

func TestSecurityMiddlewareSetsHardeningHeaders(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader("{}"))
	req.Header.Set("Authorization", "Bearer "+gateToken)

	reached := false
	rec := httptest.NewRecorder()
	SecurityMiddleware(SecurityConfig{Token: gateToken}, reachedHandler(&reached)).ServeHTTP(rec, req)

	if got := rec.Header().Get("X-Content-Type-Options"); got != "nosniff" {
		t.Errorf("X-Content-Type-Options = %q, want nosniff", got)
	}
	if got := rec.Header().Get("X-Frame-Options"); got != "DENY" {
		t.Errorf("X-Frame-Options = %q, want DENY", got)
	}
}

func TestListenAndServeUsesTLSWhenConfigured(t *testing.T) {
	srv := &http.Server{Addr: "127.0.0.1:0"}
	cfg := SecurityConfig{TLSEnabled: true, TLSCert: "no-such-cert.pem", TLSKey: "no-such-key.pem"}

	errs := make(chan error, 1)
	go func() { errs <- ListenAndServe(srv, cfg) }()

	// Serving plaintext would block here, so time out rather than hang the suite.
	select {
	case err := <-errs:
		if err == nil {
			t.Fatal("ListenAndServe() with a missing certificate returned no error")
		}
		if !strings.Contains(err.Error(), "no-such-cert.pem") {
			t.Errorf("error = %v, want it to name the certificate it could not load", err)
		}
	case <-time.After(5 * time.Second):
		_ = srv.Close()
		t.Fatal("ListenAndServe() kept serving instead of failing on the missing certificate")
	}
}

func TestStreamableHTTPKeepsGateTokenOutOfForwardedHeaders(t *testing.T) {
	var forwarded http.Header

	handler := func(ctx context.Context, msg *transport.Message) (*transport.Message, error) {
		if headers, ok := ctx.Value(client.HTTPHeadersContextKey).(http.Header); ok {
			forwarded = headers
		}
		return &transport.Message{JSONRPC: "2.0", ID: msg.ID}, nil
	}

	tr := NewStreamableHTTP(SecurityConfig{Token: gateToken}, handler, true)

	body := strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`)
	req := httptest.NewRequest(http.MethodPost, "/mcp", body)
	req.Header.Set("Authorization", "Bearer "+gateToken)
	req.Header.Set("X-Custom-Passthrough", "keep-me")

	tr.handleMCP(httptest.NewRecorder(), req)

	if forwarded == nil {
		t.Fatal("the handler received no forwarded headers")
	}
	if got := forwarded.Get("Authorization"); got != "" {
		t.Errorf("Authorization forwarded to the OData service as %q, want it stripped", got)
	}
	if got := forwarded.Get("X-Custom-Passthrough"); got != "keep-me" {
		t.Errorf("X-Custom-Passthrough = %q, want other headers still forwarded", got)
	}
}
