// Copyright (c) 2024 OData MCP Contributors
// SPDX-License-Identifier: MIT

package client

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/zmcp/odata-mcp/internal/constants"
)

type stubTokenSource struct {
	header string
	err    error
	calls  int32
}

func (s *stubTokenSource) AuthorizationHeader(context.Context) (string, error) {
	atomic.AddInt32(&s.calls, 1)
	if s.err != nil {
		return "", s.err
	}
	return s.header, nil
}

func (s *stubTokenSource) callCount() int32 {
	return atomic.LoadInt32(&s.calls)
}

func TestBuildRequestSetsBearerTokenFromTokenSource(t *testing.T) {
	source := &stubTokenSource{header: "Bearer access-1"}
	c := NewODataClient("https://example.com/odata/", false)
	c.SetTokenSource(source)

	req, err := c.buildRequest(context.Background(), constants.GET, "People", nil)
	if err != nil {
		t.Fatalf("buildRequest() error = %v", err)
	}

	if got := req.Header.Get(constants.Authorization); got != "Bearer access-1" {
		t.Errorf("Authorization = %q, want %q", got, "Bearer access-1")
	}
	if got := source.callCount(); got != 1 {
		t.Errorf("token source calls = %d, want 1", got)
	}
}

func TestBuildRequestPropagatesTokenSourceError(t *testing.T) {
	wantErr := errors.New("token endpoint returned 401")
	c := NewODataClient("https://example.com/odata/", false)
	c.SetTokenSource(&stubTokenSource{err: wantErr})

	req, err := c.buildRequest(context.Background(), constants.GET, "People", nil)
	if !errors.Is(err, wantErr) {
		t.Fatalf("buildRequest() error = %v, want it to wrap %v", err, wantErr)
	}
	if req != nil {
		t.Error("buildRequest() returned a request alongside the error")
	}
}

func TestBuildRequestPrefersBasicAuthOverTokenSource(t *testing.T) {
	source := &stubTokenSource{header: "Bearer access-1"}
	c := NewODataClient("https://example.com/odata/", false)
	c.SetBasicAuth("user", "pass")
	c.SetTokenSource(source)

	req, err := c.buildRequest(context.Background(), constants.GET, "People", nil)
	if err != nil {
		t.Fatalf("buildRequest() error = %v", err)
	}

	username, _, ok := req.BasicAuth()
	if !ok || username != "user" {
		t.Errorf("Authorization = %q, want basic auth for %q", req.Header.Get(constants.Authorization), "user")
	}
	if got := source.callCount(); got != 0 {
		t.Errorf("token source calls = %d, want 0 when basic auth is configured", got)
	}
}

func TestBuildRequestWithoutTokenSourceStaysAnonymous(t *testing.T) {
	c := NewODataClient("https://example.com/odata/", false)

	req, err := c.buildRequest(context.Background(), constants.GET, "People", nil)
	if err != nil {
		t.Fatalf("buildRequest() error = %v", err)
	}

	if got := req.Header.Get(constants.Authorization); got != "" {
		t.Errorf("Authorization = %q, want it unset", got)
	}
}

func TestGetMetadataSendsBearerToken(t *testing.T) {
	var gotAuth atomic.Value
	gotAuth.Store("")

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth.Store(r.Header.Get(constants.Authorization))
		w.Header().Set("Content-Type", constants.ContentTypeXML)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(metadataV3Fixture))
	}))
	defer server.Close()

	c := NewODataClient(server.URL+"/", false)
	c.SetTokenSource(&stubTokenSource{header: "Bearer access-1"})

	meta, err := c.GetMetadata(context.Background())
	if err != nil {
		t.Fatalf("GetMetadata() error = %v", err)
	}
	if len(meta.EntitySets) == 0 {
		t.Error("GetMetadata() returned no entity sets")
	}
	if got := gotAuth.Load().(string); got != "Bearer access-1" {
		t.Errorf("$metadata request Authorization = %q, want %q", got, "Bearer access-1")
	}
}

const metadataV3Fixture = `<?xml version="1.0" encoding="utf-8"?>
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
