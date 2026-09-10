// Copyright (c) 2024 OData MCP Contributors
// SPDX-License-Identifier: MIT

package test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/zmcp/odata-mcp/internal/client"
	"github.com/zmcp/odata-mcp/internal/config"
	"github.com/zmcp/odata-mcp/internal/registry"
	"github.com/zmcp/odata-mcp/internal/tenant"
	"github.com/zmcp/odata-mcp/internal/transport"
)

const tenantMetadataTemplate = `<?xml version="1.0" encoding="utf-8"?>
<edmx:Edmx xmlns:edmx="http://schemas.microsoft.com/ado/2007/06/edmx" Version="1.0">
  <edmx:DataServices>
    <Schema xmlns="http://schemas.microsoft.com/ado/2008/09/edm" Namespace="TenantNamespace">
      <EntityType Name="%s">
        <Key><PropertyRef Name="ID"/></Key>
        <Property Name="ID" Type="Edm.String" Nullable="false"/>
        <Property Name="Name" Type="Edm.String"/>
      </EntityType>
      <EntityContainer Name="TenantContainer">
        <EntitySet Name="%s" EntityType="TenantNamespace.%s"/>
      </EntityContainer>
    </Schema>
  </edmx:DataServices>
</edmx:Edmx>`

// tenantService is a mock OData service that only answers its own token.
type tenantService struct {
	server          *httptest.Server
	token           string
	entitySet       string
	metadataFetches int64
	dataRequests    int64
	forwardedHits   int64
}

func newTenantService(t *testing.T, token, entitySet string) *tenantService {
	t.Helper()

	service := &tenantService{token: token, entitySet: entitySet}

	service.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+service.token {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}

		if strings.HasSuffix(r.URL.Path, "/$metadata") {
			atomic.AddInt64(&service.metadataFetches, 1)
			w.Header().Set("Content-Type", "application/xml")
			fmt.Fprintf(w, tenantMetadataTemplate, entitySet, entitySet, entitySet)
			return
		}

		atomic.AddInt64(&service.dataRequests, 1)
		if r.Header.Get("Cookie") != "" || r.Header.Get("X-Forwarded-For") != "" {
			atomic.AddInt64(&service.forwardedHits, 1)
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"d": map[string]any{"results": []any{}}})
	}))

	t.Cleanup(service.server.Close)

	return service
}

func (s *tenantService) credentials() registry.Credentials {
	return registry.Credentials{ServiceURL: s.server.URL + "/", BearerToken: s.token}
}

func multiTenantConfig() *config.Config {
	return &config.Config{UniversalTool: true, ToolShrink: true}
}

func toolNames(t *testing.T, msg *transport.Message) []string {
	t.Helper()

	var result struct {
		Tools []struct {
			Name        string `json:"name"`
			Description string `json:"description"`
		} `json:"tools"`
	}

	if err := json.Unmarshal(msg.Result, &result); err != nil {
		t.Fatalf("tools/list result did not parse: %v", err)
	}

	descriptions := make([]string, 0, len(result.Tools))
	for _, tool := range result.Tools {
		descriptions = append(descriptions, tool.Description)
	}

	return descriptions
}

func listTools(t *testing.T, bridges *registry.Registry, creds registry.Credentials) []string {
	t.Helper()

	tenantBridge, err := bridges.For(context.Background(), creds)
	if err != nil {
		t.Fatalf("For() error = %v", err)
	}

	msg := &transport.Message{JSONRPC: "2.0", ID: json.RawMessage("1"), Method: "tools/list"}

	response, err := tenantBridge.HandleMessage(context.Background(), msg)
	if err != nil {
		t.Fatalf("HandleMessage() error = %v", err)
	}

	return toolNames(t, response)
}

func TestMultiTenantServesEachCallerItsOwnSchema(t *testing.T) {
	acme := newTenantService(t, "acme-token", "AcmeOrders")
	globex := newTenantService(t, "globex-token", "GlobexInvoices")

	bridges := registry.New(tenant.Factory(multiTenantConfig()))

	acmeTools := strings.Join(listTools(t, bridges, acme.credentials()), "\n")
	globexTools := strings.Join(listTools(t, bridges, globex.credentials()), "\n")

	if !strings.Contains(acmeTools, "AcmeOrders") {
		t.Errorf("acme tool description missing its own entity set:\n%s", acmeTools)
	}
	if strings.Contains(acmeTools, "GlobexInvoices") {
		t.Error("acme caller was shown the other tenant's entity set")
	}
	if !strings.Contains(globexTools, "GlobexInvoices") {
		t.Errorf("globex tool description missing its own entity set:\n%s", globexTools)
	}
	if strings.Contains(globexTools, "AcmeOrders") {
		t.Error("globex caller was shown the other tenant's entity set")
	}
}

func TestMultiTenantFetchesMetadataOncePerTenant(t *testing.T) {
	acme := newTenantService(t, "acme-token", "AcmeOrders")
	bridges := registry.New(tenant.Factory(multiTenantConfig()))

	for i := 0; i < 3; i++ {
		listTools(t, bridges, acme.credentials())
	}

	if got := atomic.LoadInt64(&acme.metadataFetches); got != 1 {
		t.Errorf("metadata fetches = %d, want 1 across three requests", got)
	}
}

func TestMultiTenantRejectsAWrongToken(t *testing.T) {
	acme := newTenantService(t, "acme-token", "AcmeOrders")
	bridges := registry.New(tenant.Factory(multiTenantConfig()))

	creds := acme.credentials()
	creds.BearerToken = "not-the-token"

	if _, err := bridges.For(context.Background(), creds); err == nil {
		t.Fatal("For() accepted a credential the service rejects")
	}

	if _, err := bridges.For(context.Background(), acme.credentials()); err != nil {
		t.Fatalf("For() with the right token error = %v", err)
	}
}

func TestMultiTenantBuildsOneBridgePerTenantUnderLoad(t *testing.T) {
	acme := newTenantService(t, "acme-token", "AcmeOrders")
	globex := newTenantService(t, "globex-token", "GlobexInvoices")

	bridges := registry.New(tenant.Factory(multiTenantConfig()))
	ctx := context.Background()

	var wg sync.WaitGroup
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()

			creds := acme.credentials()
			if i%2 == 1 {
				creds = globex.credentials()
			}

			if _, err := bridges.For(ctx, creds); err != nil {
				t.Errorf("For() error = %v", err)
			}
		}(i)
	}
	wg.Wait()

	if got := atomic.LoadInt64(&acme.metadataFetches); got != 1 {
		t.Errorf("acme metadata fetches = %d, want 1", got)
	}
	if got := atomic.LoadInt64(&globex.metadataFetches); got != 1 {
		t.Errorf("globex metadata fetches = %d, want 1", got)
	}
	if bridges.Len() != 2 {
		t.Errorf("Len() = %d, want one bridge per tenant", bridges.Len())
	}
}

func universalToolName(t *testing.T, tenantBridge registry.Bridge) string {
	t.Helper()

	msg := &transport.Message{JSONRPC: "2.0", ID: json.RawMessage("1"), Method: "tools/list"}

	response, err := tenantBridge.HandleMessage(context.Background(), msg)
	if err != nil {
		t.Fatalf("HandleMessage() error = %v", err)
	}

	var result struct {
		Tools []struct {
			Name string `json:"name"`
		} `json:"tools"`
	}
	if err := json.Unmarshal(response.Result, &result); err != nil || len(result.Tools) != 1 {
		t.Fatalf("tools/list did not return the one universal tool: %v %s", err, response.Result)
	}

	return result.Tools[0].Name
}

// The caller's HTTP headers select the credential and nothing more. Whatever
// else a browser or proxy attached must stop at the bridge.
func TestTenantBridgeForwardsNoCallerHeaders(t *testing.T) {
	acme := newTenantService(t, "acme-token", "AcmeOrders")
	bridges := registry.New(tenant.Factory(multiTenantConfig()))

	tenantBridge, err := bridges.For(context.Background(), acme.credentials())
	if err != nil {
		t.Fatalf("For() error = %v", err)
	}

	headers := http.Header{}
	headers.Set("Cookie", "session=browser-cookie")
	headers.Set("X-Forwarded-For", "10.0.0.7")
	ctx := context.WithValue(context.Background(), client.HTTPHeadersContextKey, headers)

	params := fmt.Sprintf(`{"name":%q,"arguments":{"action":"count","target":"AcmeOrders"}}`, universalToolName(t, tenantBridge))
	call := &transport.Message{JSONRPC: "2.0", ID: json.RawMessage("2"), Method: "tools/call", Params: json.RawMessage(params)}

	if _, err := tenantBridge.HandleMessage(ctx, call); err != nil {
		t.Fatalf("HandleMessage() error = %v", err)
	}

	if atomic.LoadInt64(&acme.dataRequests) == 0 {
		t.Fatal("the count never reached the service, so the test proves nothing")
	}
	if got := atomic.LoadInt64(&acme.forwardedHits); got != 0 {
		t.Errorf("the service saw the caller's Cookie or X-Forwarded-For on %d request(s)", got)
	}
}

func TestTenantConfigLeavesTheSharedConfigAlone(t *testing.T) {
	base := &config.Config{
		ServiceURL:        "https://configured.example.com/",
		Username:          "configured-user",
		Password:          "configured-password",
		OAuthClientID:     "configured-id",
		OAuthClientSecret: "configured-secret",
		UniversalTool:     true,
		MaxItems:          123,
	}

	creds := registry.Credentials{
		ServiceURL:   "https://caller.example.com/",
		ClientID:     "caller-id",
		ClientSecret: "caller-secret",
		TokenURL:     "https://caller.example.com/token",
	}

	derived := tenant.Config(base, creds)

	if base.ServiceURL != "https://configured.example.com/" || base.Username != "configured-user" {
		t.Errorf("tenant.Config mutated the shared config: %+v", base)
	}
	if derived.ServiceURL != creds.ServiceURL || derived.OAuthClientID != "caller-id" {
		t.Errorf("derived config did not take the caller's credentials: %+v", derived)
	}
	if derived.Username != "" || derived.Password != "" || derived.Cookies != nil {
		t.Error("derived config kept the configured basic or cookie auth, which would outrank the caller's token")
	}
	if !derived.UniversalTool || derived.MaxItems != 123 {
		t.Error("derived config lost the shared options")
	}
}
