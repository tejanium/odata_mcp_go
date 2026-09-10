// Copyright (c) 2024 OData MCP Contributors
// SPDX-License-Identifier: MIT

package client

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func request(t *testing.T, raw string) *http.Request {
	t.Helper()

	parsed, err := url.Parse(raw)
	if err != nil {
		t.Fatalf("url.Parse(%q) error = %v", raw, err)
	}

	return &http.Request{URL: parsed, Host: parsed.Host}
}

func TestCheckRedirect(t *testing.T) {
	tests := []struct {
		name    string
		origin  string
		target  string
		wantErr bool
	}{
		{"same host, same scheme", "https://svc.example.com/odata/", "https://svc.example.com/odata/v2/", false},
		{"same host, different case", "https://svc.example.com/a", "https://SVC.EXAMPLE.COM/b", false},
		{"http upgraded to https", "http://svc.example.com/a", "https://svc.example.com/a", false},
		{"https downgraded to http", "https://svc.example.com/a", "http://svc.example.com/a", true},
		{"different host", "https://svc.example.com/a", "https://evil.test/a", true},
		{"host that is a suffix", "https://svc.example.com/a", "https://svc.example.com.evil.test/a", true},
		{"loopback target", "https://svc.example.com/a", "http://127.0.0.1/latest/meta-data/", true},
		{"different port on the same name", "https://svc.example.com/a", "https://svc.example.com:8443/a", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := checkRedirect(request(t, tt.target), []*http.Request{request(t, tt.origin)})

			if (err != nil) != tt.wantErr {
				t.Errorf("checkRedirect(%s -> %s) error = %v, wantErr = %v", tt.origin, tt.target, err, tt.wantErr)
			}
		})
	}
}

func TestCheckRedirectAllowsTheFirstRequestAndCapsTheChain(t *testing.T) {
	if err := checkRedirect(request(t, "https://svc.example.com/a"), nil); err != nil {
		t.Errorf("checkRedirect() with no history error = %v, want nil", err)
	}

	chain := make([]*http.Request, 10)
	for i := range chain {
		chain[i] = request(t, "https://svc.example.com/a")
	}

	if err := checkRedirect(request(t, "https://svc.example.com/a"), chain); err == nil {
		t.Error("checkRedirect() followed an eleventh redirect on the same host")
	}
}

func TestClientDoesNotFollowARedirectOffHost(t *testing.T) {
	var elsewhereHits int

	elsewhere := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		elsewhereHits++
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("internal"))
	}))
	defer elsewhere.Close()

	allowed := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, elsewhere.URL+r.URL.Path, http.StatusFound)
	}))
	defer allowed.Close()

	c := NewODataClient(allowed.URL+"/", false)

	resp, err := c.httpClient.Get(allowed.URL + "/$metadata")
	if err == nil {
		_ = resp.Body.Close()
		t.Fatal("the client followed a redirect to another host")
	}
	if !strings.Contains(err.Error(), "different host") {
		t.Errorf("error = %v, want it to name the refused host change", err)
	}
	if elsewhereHits != 0 {
		t.Errorf("the other host was reached %d times, want 0", elsewhereHits)
	}
}

func TestClientStillFollowsARedirectOnTheSameHost(t *testing.T) {
	var served int

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/odata" {
			http.Redirect(w, r, "/odata/", http.StatusMovedPermanently)
			return
		}
		served++
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	}))
	defer srv.Close()

	c := NewODataClient(srv.URL+"/", false)

	resp, err := c.httpClient.Get(srv.URL + "/odata")
	if err != nil {
		t.Fatalf("same-host redirect was refused: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK || served != 1 {
		t.Errorf("status = %d served = %d, want the trailing-slash redirect followed", resp.StatusCode, served)
	}
}
