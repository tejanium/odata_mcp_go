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
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/zmcp/odata-mcp/internal/bridge"
	"github.com/zmcp/odata-mcp/internal/config"
	"github.com/zmcp/odata-mcp/internal/transport"
)

const hintsMetadataXML = `<?xml version="1.0" encoding="utf-8"?>
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

func newMetadataOnlyService(t *testing.T) *httptest.Server {
	t.Helper()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/$metadata") {
			w.Header().Set("Content-Type", "application/xml")
			fmt.Fprint(w, hintsMetadataXML)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"value":[]}`)
	}))
	t.Cleanup(server.Close)

	return server
}

func universalConfig(serviceURL string) *config.Config {
	return &config.Config{
		ServiceURL:    serviceURL,
		UniversalTool: true,
		MaxItems:      100,
		SortTools:     true,
	}
}

func universalTool(t *testing.T, cfg *config.Config) (*bridge.ODataMCPBridge, string) {
	t.Helper()

	odataBridge, err := bridge.NewODataMCPBridge(cfg)
	require.NoError(t, err)

	tools := odataBridge.GetServer().GetTools()
	require.Len(t, tools, 1, "universal mode should register exactly one tool")

	return odataBridge, tools[0].Description
}

func TestUniversalToolDescriptionCarriesCLIHint(t *testing.T) {
	service := newMetadataOnlyService(t)

	cfg := universalConfig(service.URL + "/odata/")
	cfg.Hint = `{"notes":["Contacts carries no email; use ContactSearch"],` +
		`"known_issues":["eq on _Search columns is case sensitive"]}`

	_, description := universalTool(t, cfg)

	assert.Contains(t, description, "Service hints:")
	assert.Contains(t, description, "Contacts carries no email; use ContactSearch")
	assert.Contains(t, description, "eq on _Search columns is case sensitive")
}

func TestUniversalToolDescriptionOmitsHintSectionWhenNoneMatch(t *testing.T) {
	service := newMetadataOnlyService(t)

	_, description := universalTool(t, universalConfig(service.URL+"/odata/"))

	assert.NotContains(t, description, "Service hints:")
	assert.Contains(t, description, "Entities:", "the rest of the description should be unaffected")
}

func TestUniversalToolAdvertisesInfoAction(t *testing.T) {
	service := newMetadataOnlyService(t)

	odataBridge, description := universalTool(t, universalConfig(service.URL+"/odata/"))

	assert.Contains(t, description, "info   - Without target: service details")

	schema := odataBridge.GetServer().GetTools()[0].InputSchema
	properties := schema["properties"].(map[string]any)
	action := properties["action"].(map[string]any)
	assert.Contains(t, action["enum"], "info")

	// info takes no target, so target cannot stay required.
	assert.Equal(t, []string{"action"}, schema["required"])
}

// callUniversalTool drives the tool through the MCP server, the same path a
// client takes, and returns the tool's text result or the JSON-RPC error.
func callUniversalTool(t *testing.T, odataBridge *bridge.ODataMCPBridge, args map[string]any) (string, *transport.Error) {
	t.Helper()

	params, err := json.Marshal(map[string]any{
		"name":      odataBridge.GetServer().GetTools()[0].Name,
		"arguments": args,
	})
	require.NoError(t, err)

	response, err := odataBridge.GetServer().HandleMessage(context.Background(), &transport.Message{
		JSONRPC: "2.0",
		ID:      json.RawMessage("1"),
		Method:  "tools/call",
		Params:  params,
	})
	require.NoError(t, err)
	require.NotNil(t, response, "the server returned no response")

	if response.Error != nil {
		return "", response.Error
	}

	var result struct {
		Content []struct {
			Text string `json:"text"`
		} `json:"content"`
	}
	require.NoError(t, json.Unmarshal(response.Result, &result))
	require.NotEmpty(t, result.Content)

	return result.Content[0].Text, nil
}

func TestUniversalInfoActionReturnsHints(t *testing.T) {
	service := newMetadataOnlyService(t)

	cfg := universalConfig(service.URL + "/odata/")
	cfg.Hint = `{"notes":["Contacts carries no email; use ContactSearch"]}`

	odataBridge, _ := universalTool(t, cfg)

	text, rpcErr := callUniversalTool(t, odataBridge, map[string]any{"action": "info"})
	require.Nil(t, rpcErr, "info should not require a target")

	var info map[string]any
	require.NoError(t, json.Unmarshal([]byte(text), &info))

	assert.Equal(t, "universal", info["mode"])
	assert.EqualValues(t, 1, info["entity_sets"])

	hints, ok := info["implementation_hints"].(map[string]any)
	require.True(t, ok, "info should carry implementation_hints, got %v", info["implementation_hints"])
	assert.Contains(t, fmt.Sprint(hints["notes"]), "Contacts carries no email")
}

func TestUniversalActionsStillRequireATarget(t *testing.T) {
	service := newMetadataOnlyService(t)

	odataBridge, _ := universalTool(t, universalConfig(service.URL+"/odata/"))

	_, rpcErr := callUniversalTool(t, odataBridge, map[string]any{"action": "list"})
	require.NotNil(t, rpcErr, "list without a target should be an error")
	assert.Contains(t, fmt.Sprint(rpcErr.Message, string(rpcErr.Data)), "target")
}
