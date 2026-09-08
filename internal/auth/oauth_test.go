// Copyright (c) 2024 OData MCP Contributors
// SPDX-License-Identifier: MIT

package auth

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

const (
	testClientID     = "test-client-id"
	testClientSecret = "test-client-secret"
	testScope        = "http://www.example.com/auth-scope/APIRead"
)

// tokenServer records what the token endpoint received and replies with body.
type tokenServer struct {
	*httptest.Server

	mu       sync.Mutex
	calls    int32
	lastForm url.Values
	lastAuth string
	lastType string
}

func newTokenServer(t *testing.T, handler func(callNo int32) (int, string)) *tokenServer {
	t.Helper()

	ts := &tokenServer{}
	ts.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callNo := atomic.AddInt32(&ts.calls, 1)

		if err := r.ParseForm(); err != nil {
			t.Errorf("token endpoint got an unparseable body: %v", err)
		}

		ts.mu.Lock()
		ts.lastForm = r.PostForm
		ts.lastAuth = r.Header.Get("Authorization")
		ts.lastType = r.Header.Get("Content-Type")
		ts.mu.Unlock()

		status, body := handler(callNo)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		fmt.Fprint(w, body)
	}))
	t.Cleanup(ts.Close)

	return ts
}

func (ts *tokenServer) callCount() int32 {
	return atomic.LoadInt32(&ts.calls)
}

func (ts *tokenServer) form() url.Values {
	ts.mu.Lock()
	defer ts.mu.Unlock()
	return ts.lastForm
}

func (ts *tokenServer) authHeader() string {
	ts.mu.Lock()
	defer ts.mu.Unlock()
	return ts.lastAuth
}

func (ts *tokenServer) contentType() string {
	ts.mu.Lock()
	defer ts.mu.Unlock()
	return ts.lastType
}

func tokenJSON(accessToken, tokenType string, expiresIn interface{}) string {
	payload := map[string]interface{}{"access_token": accessToken}
	if tokenType != "" {
		payload["token_type"] = tokenType
	}
	if expiresIn != nil {
		payload["expires_in"] = expiresIn
	}
	encoded, _ := json.Marshal(payload)
	return string(encoded)
}

func alwaysToken(accessToken string) func(int32) (int, string) {
	return func(int32) (int, string) {
		return http.StatusOK, tokenJSON(accessToken, "bearer", 300)
	}
}

func newTestSource(t *testing.T, config OAuthConfig) *OAuthTokenSource {
	t.Helper()

	source, err := NewOAuthTokenSource(config)
	if err != nil {
		t.Fatalf("NewOAuthTokenSource() error = %v", err)
	}
	return source
}

func TestOAuthConfigValidate(t *testing.T) {
	base := OAuthConfig{
		ClientID:     testClientID,
		ClientSecret: testClientSecret,
		TokenURL:     "https://example.com/oauth/token",
	}

	tests := []struct {
		name        string
		mutate      func(*OAuthConfig)
		wantErr     bool
		errContains string
	}{
		{
			name:   "complete configuration",
			mutate: func(*OAuthConfig) {},
		},
		{
			name:   "body client auth is allowed",
			mutate: func(c *OAuthConfig) { c.ClientAuth = ClientAuthBody },
		},
		{
			name:   "basic client auth is allowed",
			mutate: func(c *OAuthConfig) { c.ClientAuth = ClientAuthBasic },
		},
		{
			name:        "missing client ID",
			mutate:      func(c *OAuthConfig) { c.ClientID = "" },
			wantErr:     true,
			errContains: "client ID",
		},
		{
			name:        "missing client secret",
			mutate:      func(c *OAuthConfig) { c.ClientSecret = "" },
			wantErr:     true,
			errContains: "client secret",
		},
		{
			name:        "missing token URL",
			mutate:      func(c *OAuthConfig) { c.TokenURL = "" },
			wantErr:     true,
			errContains: "token URL",
		},
		{
			name: "every missing field is named at once",
			mutate: func(c *OAuthConfig) {
				c.ClientID = ""
				c.ClientSecret = ""
				c.TokenURL = ""
			},
			wantErr:     true,
			errContains: "client ID, client secret, token URL",
		},
		{
			name:        "non-http token URL scheme",
			mutate:      func(c *OAuthConfig) { c.TokenURL = "ftp://example.com/token" },
			wantErr:     true,
			errContains: "must be http or https",
		},
		{
			name:        "token URL without a host",
			mutate:      func(c *OAuthConfig) { c.TokenURL = "https:///token" },
			wantErr:     true,
			errContains: "no host",
		},
		{
			name:        "unknown client auth method",
			mutate:      func(c *OAuthConfig) { c.ClientAuth = "jwt" },
			wantErr:     true,
			errContains: "must be \"basic\" or \"body\"",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := base
			tt.mutate(&config)

			err := config.Validate()
			if tt.wantErr {
				if err == nil {
					t.Fatalf("Validate() = nil, want error containing %q", tt.errContains)
				}
				if !strings.Contains(err.Error(), tt.errContains) {
					t.Errorf("Validate() error = %q, want it to contain %q", err, tt.errContains)
				}
				return
			}
			if err != nil {
				t.Errorf("Validate() error = %v, want nil", err)
			}
		})
	}
}

func TestNewOAuthTokenSourceRejectsInvalidConfig(t *testing.T) {
	source, err := NewOAuthTokenSource(OAuthConfig{ClientID: testClientID})
	if err == nil {
		t.Fatal("NewOAuthTokenSource() = nil error, want a validation error")
	}
	if source != nil {
		t.Errorf("NewOAuthTokenSource() = %v, want nil source alongside the error", source)
	}
}

func TestAuthorizationHeaderUsesBasicClientAuthByDefault(t *testing.T) {
	server := newTokenServer(t, alwaysToken("access-1"))
	source := newTestSource(t, OAuthConfig{
		ClientID:     testClientID,
		ClientSecret: testClientSecret,
		TokenURL:     server.URL,
		Scope:        testScope,
	})

	header, err := source.AuthorizationHeader(context.Background())
	if err != nil {
		t.Fatalf("AuthorizationHeader() error = %v", err)
	}
	if header != "Bearer access-1" {
		t.Errorf("AuthorizationHeader() = %q, want %q", header, "Bearer access-1")
	}

	username, password, ok := basicCredentials(server.authHeader())
	if !ok {
		t.Fatalf("token request Authorization = %q, want HTTP Basic", server.authHeader())
	}
	if username != testClientID || password != testClientSecret {
		t.Errorf("basic credentials = %q/%q, want %q/%q",
			username, password, testClientID, testClientSecret)
	}

	form := server.form()
	if got := form.Get("grant_type"); got != grantClientCredentials {
		t.Errorf("grant_type = %q, want %q", got, grantClientCredentials)
	}
	if got := form.Get("scope"); got != testScope {
		t.Errorf("scope = %q, want %q", got, testScope)
	}
	if form.Has("client_secret") {
		t.Error("client_secret was sent in the body as well as the Basic header")
	}
	if got := server.contentType(); got != "application/x-www-form-urlencoded" {
		t.Errorf("Content-Type = %q, want application/x-www-form-urlencoded", got)
	}
}

func TestAuthorizationHeaderWithBodyClientAuth(t *testing.T) {
	server := newTokenServer(t, alwaysToken("access-1"))
	source := newTestSource(t, OAuthConfig{
		ClientID:     testClientID,
		ClientSecret: testClientSecret,
		TokenURL:     server.URL,
		ClientAuth:   ClientAuthBody,
	})

	if _, err := source.AuthorizationHeader(context.Background()); err != nil {
		t.Fatalf("AuthorizationHeader() error = %v", err)
	}

	if got := server.authHeader(); got != "" {
		t.Errorf("token request Authorization = %q, want it unset for body client auth", got)
	}

	form := server.form()
	if got := form.Get("client_id"); got != testClientID {
		t.Errorf("client_id = %q, want %q", got, testClientID)
	}
	if got := form.Get("client_secret"); got != testClientSecret {
		t.Errorf("client_secret = %q, want %q", got, testClientSecret)
	}
}

func TestAuthorizationHeaderOmitsEmptyScope(t *testing.T) {
	server := newTokenServer(t, alwaysToken("access-1"))
	source := newTestSource(t, OAuthConfig{
		ClientID:     testClientID,
		ClientSecret: testClientSecret,
		TokenURL:     server.URL,
	})

	if _, err := source.AuthorizationHeader(context.Background()); err != nil {
		t.Fatalf("AuthorizationHeader() error = %v", err)
	}

	if server.form().Has("scope") {
		t.Error("scope was sent even though none was configured")
	}
}

func TestAuthorizationHeaderCachesToken(t *testing.T) {
	server := newTokenServer(t, alwaysToken("access-1"))
	source := newTestSource(t, OAuthConfig{
		ClientID:     testClientID,
		ClientSecret: testClientSecret,
		TokenURL:     server.URL,
	})

	for i := 0; i < 3; i++ {
		header, err := source.AuthorizationHeader(context.Background())
		if err != nil {
			t.Fatalf("AuthorizationHeader() call %d error = %v", i+1, err)
		}
		if header != "Bearer access-1" {
			t.Errorf("AuthorizationHeader() call %d = %q, want %q", i+1, header, "Bearer access-1")
		}
	}

	if got := server.callCount(); got != 1 {
		t.Errorf("token endpoint calls = %d, want 1", got)
	}
}

func TestAuthorizationHeaderRefreshesExpiredToken(t *testing.T) {
	server := newTokenServer(t, func(callNo int32) (int, string) {
		return http.StatusOK, tokenJSON(fmt.Sprintf("access-%d", callNo), "Bearer", 300)
	})
	source := newTestSource(t, OAuthConfig{
		ClientID:     testClientID,
		ClientSecret: testClientSecret,
		TokenURL:     server.URL,
	})

	clock := time.Now()
	source.now = func() time.Time { return clock }

	if _, err := source.AuthorizationHeader(context.Background()); err != nil {
		t.Fatalf("AuthorizationHeader() error = %v", err)
	}

	// 300s lifetime, so the margin is 60s and the token is reused at 239s.
	clock = clock.Add(239 * time.Second)
	header, err := source.AuthorizationHeader(context.Background())
	if err != nil {
		t.Fatalf("AuthorizationHeader() error = %v", err)
	}
	if header != "Bearer access-1" {
		t.Errorf("before the margin, AuthorizationHeader() = %q, want the cached %q", header, "Bearer access-1")
	}

	clock = clock.Add(2 * time.Second)
	header, err = source.AuthorizationHeader(context.Background())
	if err != nil {
		t.Fatalf("AuthorizationHeader() error = %v", err)
	}
	if header != "Bearer access-2" {
		t.Errorf("inside the margin, AuthorizationHeader() = %q, want a refreshed %q", header, "Bearer access-2")
	}
	if got := server.callCount(); got != 2 {
		t.Errorf("token endpoint calls = %d, want 2", got)
	}
}

func TestAuthorizationHeaderTokenTypes(t *testing.T) {
	tests := []struct {
		name       string
		tokenType  string
		wantHeader string
	}{
		{name: "lowercase bearer is normalized", tokenType: "bearer", wantHeader: "Bearer access-1"},
		{name: "mixed case bearer is normalized", tokenType: "BeArEr", wantHeader: "Bearer access-1"},
		{name: "absent token type defaults to bearer", tokenType: "", wantHeader: "Bearer access-1"},
		{name: "other schemes pass through", tokenType: "MAC", wantHeader: "MAC access-1"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := newTokenServer(t, func(int32) (int, string) {
				return http.StatusOK, tokenJSON("access-1", tt.tokenType, 300)
			})
			source := newTestSource(t, OAuthConfig{
				ClientID:     testClientID,
				ClientSecret: testClientSecret,
				TokenURL:     server.URL,
			})

			header, err := source.AuthorizationHeader(context.Background())
			if err != nil {
				t.Fatalf("AuthorizationHeader() error = %v", err)
			}
			if header != tt.wantHeader {
				t.Errorf("AuthorizationHeader() = %q, want %q", header, tt.wantHeader)
			}
		})
	}
}

func TestAuthorizationHeaderExpiresInVariants(t *testing.T) {
	tests := []struct {
		name         string
		expiresIn    interface{}
		wantLifetime time.Duration
		wantErr      bool
	}{
		{name: "numeric", expiresIn: 300, wantLifetime: 300 * time.Second},
		{name: "string", expiresIn: "300", wantLifetime: 300 * time.Second},
		{name: "absent", expiresIn: nil, wantLifetime: defaultExpiresIn},
		{name: "zero falls back to the default", expiresIn: 0, wantLifetime: defaultExpiresIn},
		{name: "negative falls back to the default", expiresIn: -1, wantLifetime: defaultExpiresIn},
		{name: "non-numeric", expiresIn: "soon", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := newTokenServer(t, func(int32) (int, string) {
				return http.StatusOK, tokenJSON("access-1", "Bearer", tt.expiresIn)
			})
			source := newTestSource(t, OAuthConfig{
				ClientID:     testClientID,
				ClientSecret: testClientSecret,
				TokenURL:     server.URL,
			})

			clock := time.Now()
			source.now = func() time.Time { return clock }

			_, err := source.AuthorizationHeader(context.Background())
			if tt.wantErr {
				if err == nil {
					t.Fatal("AuthorizationHeader() = nil error, want a parse error")
				}
				if !strings.Contains(err.Error(), "expires_in") {
					t.Errorf("AuthorizationHeader() error = %q, want it to mention expires_in", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("AuthorizationHeader() error = %v", err)
			}

			want := clock.Add(tt.wantLifetime - refreshMargin(tt.wantLifetime))
			if !source.expiresAt.Equal(want) {
				t.Errorf("expiresAt = %v, want %v", source.expiresAt, want)
			}
		})
	}
}

func TestAuthorizationHeaderErrors(t *testing.T) {
	tests := []struct {
		name        string
		status      int
		body        string
		errContains string
	}{
		{
			name:        "unauthorized client",
			status:      http.StatusUnauthorized,
			body:        `{"error":"invalid_client"}`,
			errContains: "returned 401",
		},
		{
			name:        "server error",
			status:      http.StatusInternalServerError,
			body:        "upstream exploded",
			errContains: "returned 500",
		},
		{
			name:        "malformed JSON",
			status:      http.StatusOK,
			body:        "<html>not json</html>",
			errContains: "not valid JSON",
		},
		{
			name:        "no access token",
			status:      http.StatusOK,
			body:        `{"token_type":"Bearer","expires_in":300}`,
			errContains: "no access_token",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := newTokenServer(t, func(int32) (int, string) {
				return tt.status, tt.body
			})
			source := newTestSource(t, OAuthConfig{
				ClientID:     testClientID,
				ClientSecret: testClientSecret,
				TokenURL:     server.URL,
			})

			header, err := source.AuthorizationHeader(context.Background())
			if err == nil {
				t.Fatalf("AuthorizationHeader() = %q, want an error", header)
			}
			if header != "" {
				t.Errorf("AuthorizationHeader() = %q, want an empty header alongside the error", header)
			}
			if !strings.Contains(err.Error(), tt.errContains) {
				t.Errorf("AuthorizationHeader() error = %q, want it to contain %q", err, tt.errContains)
			}
			if strings.Contains(err.Error(), testClientSecret) {
				t.Error("AuthorizationHeader() error leaked the client secret")
			}
		})
	}
}

func TestAuthorizationHeaderTruncatesLongErrorBody(t *testing.T) {
	server := newTokenServer(t, func(int32) (int, string) {
		return http.StatusBadRequest, strings.Repeat("x", errorBodyLimit*4)
	})
	source := newTestSource(t, OAuthConfig{
		ClientID:     testClientID,
		ClientSecret: testClientSecret,
		TokenURL:     server.URL,
	})

	_, err := source.AuthorizationHeader(context.Background())
	if err == nil {
		t.Fatal("AuthorizationHeader() = nil error, want an error")
	}
	if len(err.Error()) > errorBodyLimit*2 {
		t.Errorf("error length = %d, want the body truncated near %d", len(err.Error()), errorBodyLimit)
	}
	if !strings.HasSuffix(err.Error(), "...") {
		t.Errorf("error = %q, want it to end in an ellipsis", err)
	}
}

// Real access tokens run well past the error-message cap; an early version
// read only that many bytes and failed to parse every successful response.
func TestAuthorizationHeaderAcceptsTokenLongerThanErrorLimit(t *testing.T) {
	longToken := strings.Repeat("a", errorBodyLimit*2)
	server := newTokenServer(t, func(int32) (int, string) {
		return http.StatusOK, tokenJSON(longToken, "bearer", 300)
	})
	source := newTestSource(t, OAuthConfig{
		ClientID:     testClientID,
		ClientSecret: testClientSecret,
		TokenURL:     server.URL,
	})

	header, err := source.AuthorizationHeader(context.Background())
	if err != nil {
		t.Fatalf("AuthorizationHeader() error = %v", err)
	}
	if header != "Bearer "+longToken {
		t.Errorf("AuthorizationHeader() returned a truncated token of %d bytes, want %d",
			len(header), len("Bearer "+longToken))
	}
}

func TestAuthorizationHeaderHonoursContextCancellation(t *testing.T) {
	server := newTokenServer(t, alwaysToken("access-1"))
	source := newTestSource(t, OAuthConfig{
		ClientID:     testClientID,
		ClientSecret: testClientSecret,
		TokenURL:     server.URL,
	})

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if _, err := source.AuthorizationHeader(ctx); err == nil {
		t.Fatal("AuthorizationHeader() = nil error, want a cancellation error")
	}
}

func TestAuthorizationHeaderFetchesOnceUnderConcurrency(t *testing.T) {
	server := newTokenServer(t, func(callNo int32) (int, string) {
		time.Sleep(20 * time.Millisecond)
		return http.StatusOK, tokenJSON(fmt.Sprintf("access-%d", callNo), "Bearer", 300)
	})
	source := newTestSource(t, OAuthConfig{
		ClientID:     testClientID,
		ClientSecret: testClientSecret,
		TokenURL:     server.URL,
	})

	const goroutines = 25
	headers := make([]string, goroutines)
	errs := make([]error, goroutines)

	var start, done sync.WaitGroup
	start.Add(1)
	done.Add(goroutines)

	for i := 0; i < goroutines; i++ {
		go func(i int) {
			defer done.Done()
			start.Wait()
			headers[i], errs[i] = source.AuthorizationHeader(context.Background())
		}(i)
	}

	start.Done()
	done.Wait()

	for i, err := range errs {
		if err != nil {
			t.Fatalf("goroutine %d AuthorizationHeader() error = %v", i, err)
		}
		if headers[i] != "Bearer access-1" {
			t.Errorf("goroutine %d header = %q, want %q", i, headers[i], "Bearer access-1")
		}
	}

	if got := server.callCount(); got != 1 {
		t.Errorf("token endpoint calls = %d, want 1 request shared by all callers", got)
	}
}

func TestRefreshMargin(t *testing.T) {
	tests := []struct {
		name     string
		lifetime time.Duration
		want     time.Duration
	}{
		{name: "long lifetime is capped", lifetime: time.Hour, want: maxRefreshMargin},
		{name: "five minutes gets a fifth", lifetime: 300 * time.Second, want: 60 * time.Second},
		{name: "one minute gets a fifth", lifetime: time.Minute, want: 12 * time.Second},
		{name: "short lifetime gets the floor", lifetime: 20 * time.Second, want: minRefreshMargin},
		{name: "very short lifetime keeps half", lifetime: 6 * time.Second, want: 3 * time.Second},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := refreshMargin(tt.lifetime); got != tt.want {
				t.Errorf("refreshMargin(%v) = %v, want %v", tt.lifetime, got, tt.want)
			}
		})
	}
}

func TestRefreshMarginNeverExceedsHalfTheLifetime(t *testing.T) {
	for _, lifetime := range []time.Duration{
		time.Second, 2 * time.Second, 5 * time.Second, 11 * time.Second,
		30 * time.Second, time.Minute, 300 * time.Second, time.Hour,
	} {
		margin := refreshMargin(lifetime)
		if margin > lifetime/2 {
			t.Errorf("refreshMargin(%v) = %v, want no more than half the lifetime", lifetime, margin)
		}
		if lifetime-margin <= 0 {
			t.Errorf("refreshMargin(%v) = %v, which leaves no usable token window", lifetime, margin)
		}
	}
}

func basicCredentials(header string) (string, string, bool) {
	req := &http.Request{Header: http.Header{"Authorization": []string{header}}}
	return req.BasicAuth()
}
