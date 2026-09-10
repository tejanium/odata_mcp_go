// Copyright (c) 2024 OData MCP Contributors
// SPDX-License-Identifier: MIT

package bridge

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/zmcp/odata-mcp/internal/config"
	"github.com/zmcp/odata-mcp/internal/models"
)

func describedBridge(cfg *config.Config) *ODataMCPBridge {
	b := filteredBridge(cfg)
	b.metadata.EntityTypes["Person"].Properties = []*models.EntityProperty{
		{Name: "PersonCode", Type: "Edm.String", Nullable: false, IsKey: true},
		{Name: "FamilyName", Type: "Edm.String", Nullable: true},
		{Name: "DateOfBirth", Type: "Edm.DateTime", Nullable: true},
	}
	b.metadata.EntityTypes["Person"].NavigationProps = []*models.NavigationProperty{{Name: "PeopleToCompCurrent"}}
	b.metadata.FunctionImports["Report"].Parameters = []*models.FunctionParameter{{Name: "Year", Type: "Edm.Int32", Mode: "In"}}

	return b
}

func infoFor(t *testing.T, b *ODataMCPBridge, target string) map[string]any {
	t.Helper()

	result, err := b.handleUniversalTool(context.Background(), map[string]any{"action": "info", "target": target})
	if err != nil {
		t.Fatalf("info %s: %v", target, err)
	}

	text, ok := result.(string)
	if !ok {
		t.Fatalf("info %s returned %T, want JSON text", target, result)
	}

	var info map[string]any
	if err := json.Unmarshal([]byte(text), &info); err != nil {
		t.Fatalf("info %s is not JSON: %v", target, err)
	}

	return info
}

func TestInfoWithATargetDescribesTheEntitySet(t *testing.T) {
	info := infoFor(t, describedBridge(&config.Config{}), "People")

	if got := info["key"].([]any); len(got) != 1 || got[0] != "PersonCode" {
		t.Errorf("key = %v, want [PersonCode]", got)
	}

	properties := info["properties"].([]any)
	if len(properties) != 3 || properties[2].(map[string]any)["name"] != "DateOfBirth" || properties[2].(map[string]any)["type"] != "Edm.DateTime" {
		t.Errorf("properties = %v, want the three declared in order", properties)
	}

	if got := info["operations"].([]any); len(got) != 6 {
		t.Errorf("operations = %v, want list, get, count, create, update, delete", got)
	}

	if got := info["navigation_properties"].([]any); len(got) != 1 || got[0] != "PeopleToCompCurrent" {
		t.Errorf("navigation_properties = %v", got)
	}

	if _, present := info["entities"]; present {
		t.Error("a target info must not dump the whole catalog")
	}
}

func TestInfoWithATargetRespectsReadOnlyAndFilters(t *testing.T) {
	b := describedBridge(&config.Config{ReadOnly: true, AllowedEntities: []string{"People"}})

	info := infoFor(t, b, "People")
	if got := info["operations"].([]any); len(got) != 3 {
		t.Errorf("operations under --read-only = %v, want list, get, count", got)
	}
	if _, present := info["navigation_properties"]; present {
		t.Error("navigation properties advertised while --entities forbids following them")
	}

	if _, err := b.handleUniversalTool(context.Background(), map[string]any{"action": "info", "target": "Salaries"}); err == nil {
		t.Error("info described an entity set outside --entities")
	}
}

func TestInfoWithAFunctionTargetDescribesTheFunction(t *testing.T) {
	info := infoFor(t, describedBridge(&config.Config{ReadOnlyButFunctions: true}), "Report")

	if info["http_method"] != "GET" || info["callable"] != true {
		t.Errorf("Report = %v, want a callable GET", info)
	}

	parameters := info["parameters"].([]any)
	if len(parameters) != 1 || parameters[0].(map[string]any)["name"] != "Year" {
		t.Errorf("parameters = %v", parameters)
	}

	action := infoFor(t, describedBridge(&config.Config{ReadOnlyButFunctions: true}), "Recalculate")
	if action["callable"] != false {
		t.Error("a POST action reported callable under --read-only-but-functions")
	}
}

func TestInfoWithoutATargetStillDescribesTheService(t *testing.T) {
	result, err := describedBridge(&config.Config{}).handleUniversalTool(context.Background(), map[string]any{"action": "info"})
	if err != nil {
		t.Fatal(err)
	}

	text, ok := result.(string)
	if !ok || !strings.Contains(text, `"entities"`) {
		t.Errorf("bare info = %v, want the catalog as JSON text", result)
	}
}
