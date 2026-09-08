// Copyright (c) 2024 OData MCP Contributors
// SPDX-License-Identifier: MIT

package test

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/zmcp/odata-mcp/internal/bridge"
	"github.com/zmcp/odata-mcp/internal/config"
)

const oauthMetadataXML = `<?xml version="1.0" encoding="utf-8"?>
<edmx:Edmx Version="1.0" xmlns:edmx="http://schemas.microsoft.com/ado/2007/06/edmx">
  <edmx:DataServices m:DataServiceVersion="3.0" xmlns:m="http://schemas.microsoft.com/ado/2007/08/dataservices/metadata">
    <Schema Namespace="Test" xmlns="http://schemas.microsoft.com/ado/2009/11/edm">
      <EntityType Name="Person">
        <Key><PropertyRef Name="PersonCode"/></Key>
        <Property Name="PersonCode" Type="Edm.String" Nullable="false"/>
        <Property Name="FamilyName" Type="Edm.String" Nullable="true"/>
      </EntityType>
      <EntityContainer Name="TestContainer" m:IsDefaultEntityContainer="true">
        <EntitySet Name="People" EntityType="Test.Person"/>
      </EntityContainer>
    </Schema>
  </edmx:DataServices>
</edmx:Edmx>`

// oauthProtectedService is an OData service that answers only to a bearer
// token it issues itself, so the bridge's whole auth path is exercised.
type oauthProtectedService struct {
	*httptest.Server

	mu               sync.Mutex
	tokenRequests    int
	odataRequests    int
	unauthorizedHits int
	lastClientID     string
	lastScope        string
}

func newOAuthProtectedService(t *testing.T) *oauthProtectedService {
	t.Helper()

	svc := &oauthProtectedService{}
	mux := http.NewServeMux()

	mux.HandleFunc("/oauth/token", func(w http.ResponseWriter, r *http.Request) {
		require.NoError(t, r.ParseForm())

		clientID, clientSecret, ok := r.BasicAuth()
		if !ok {
			clientID = r.PostForm.Get("client_id")
			clientSecret = r.PostForm.Get("client_secret")
		}

		svc.mu.Lock()
		svc.tokenRequests++
		issued := svc.tokenRequests
		svc.lastClientID = clientID
		svc.lastScope = r.PostForm.Get("scope")
		svc.mu.Unlock()

		if clientID != "svc-client" || clientSecret != "svc-secret" {
			w.WriteHeader(http.StatusUnauthorized)
			fmt.Fprint(w, `{"error":"invalid_client"}`)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"access_token":"issued-%d","token_type":"bearer","expires_in":300}`, issued)
	})

	mux.HandleFunc("/odata/", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer issued-1" {
			svc.mu.Lock()
			svc.unauthorizedHits++
			svc.mu.Unlock()

			w.WriteHeader(http.StatusUnauthorized)
			fmt.Fprint(w, `{"error":{"code":"401","message":{"value":"missing bearer token"}}}`)
			return
		}

		svc.mu.Lock()
		svc.odataRequests++
		svc.mu.Unlock()

		if strings.HasSuffix(r.URL.Path, "/$metadata") {
			w.Header().Set("Content-Type", "application/xml")
			fmt.Fprint(w, oauthMetadataXML)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"value":[{"PersonCode":"CEZ01","FamilyName":"Example"}]}`)
	})

	svc.Server = httptest.NewServer(mux)
	t.Cleanup(svc.Close)

	return svc
}

func (s *oauthProtectedService) counts() (tokens, odata, unauthorized int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.tokenRequests, s.odataRequests, s.unauthorizedHits
}

func (s *oauthProtectedService) lastTokenRequest() (clientID, scope string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.lastClientID, s.lastScope
}

func oauthBridgeConfig(svc *oauthProtectedService) *config.Config {
	return &config.Config{
		ServiceURL:        svc.URL + "/odata/",
		OAuthClientID:     "svc-client",
		OAuthClientSecret: "svc-secret",
		OAuthTokenURL:     svc.URL + "/oauth/token",
		OAuthScope:        "http://www.example.com/auth-scope/APIRead",
		MaxItems:          100,
		SortTools:         true,
	}
}

func TestBridgeAuthenticatesWithOAuthClientCredentials(t *testing.T) {
	svc := newOAuthProtectedService(t)

	odataBridge, err := bridge.NewODataMCPBridge(oauthBridgeConfig(svc))
	require.NoError(t, err)

	info, err := odataBridge.GetTraceInfo()
	require.NoError(t, err)

	assert.Equal(t, "OAuth 2.0 client credentials (client: svc-client)", info.Authentication)
	assert.Equal(t, 1, info.MetadataSummary.EntitySets, "the protected $metadata should have been parsed")

	tokens, odata, unauthorized := svc.counts()
	assert.Equal(t, 1, tokens, "the token should be fetched once and cached")
	assert.Greater(t, odata, 0, "the OData service should have accepted authenticated requests")
	assert.Equal(t, 0, unauthorized, "no request should have reached the service without a token")

	clientID, scope := svc.lastTokenRequest()
	assert.Equal(t, "svc-client", clientID)
	assert.Equal(t, "http://www.example.com/auth-scope/APIRead", scope)
}

func TestBridgeUsesBodyClientAuthWhenRequested(t *testing.T) {
	svc := newOAuthProtectedService(t)

	cfg := oauthBridgeConfig(svc)
	cfg.OAuthClientAuth = "body"

	_, err := bridge.NewODataMCPBridge(cfg)
	require.NoError(t, err)

	clientID, _ := svc.lastTokenRequest()
	assert.Equal(t, "svc-client", clientID, "credentials should arrive as form fields")
}

func TestBridgeReportsBadOAuthCredentials(t *testing.T) {
	svc := newOAuthProtectedService(t)

	cfg := oauthBridgeConfig(svc)
	cfg.OAuthClientSecret = "wrong-secret"

	_, err := bridge.NewODataMCPBridge(cfg)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "401")
	assert.NotContains(t, err.Error(), "wrong-secret", "the error must not leak the client secret")
}

// An incomplete OAuth config cannot reach the bridge through the CLI, which
// validates first; if it does, the request goes out unauthenticated and fails.
func TestBridgeWithIncompleteOAuthConfigFailsUnauthenticated(t *testing.T) {
	svc := newOAuthProtectedService(t)

	cfg := oauthBridgeConfig(svc)
	cfg.OAuthTokenURL = ""

	_, err := bridge.NewODataMCPBridge(cfg)
	require.Error(t, err)

	tokens, _, unauthorized := svc.counts()
	assert.Equal(t, 0, tokens, "no token should be requested without a token URL")
	assert.Greater(t, unauthorized, 0, "the service should have rejected the unauthenticated request")
}
