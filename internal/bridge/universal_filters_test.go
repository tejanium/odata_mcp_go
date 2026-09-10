// Copyright (c) 2024 OData MCP Contributors
// SPDX-License-Identifier: MIT

package bridge

import (
	"context"
	"strings"
	"testing"

	"github.com/zmcp/odata-mcp/internal/config"
	"github.com/zmcp/odata-mcp/internal/hint"
	"github.com/zmcp/odata-mcp/internal/models"
)

// filteredBridge has no client on purpose: every call below must be refused
// before anything would reach the service, or it panics on the nil client.
func filteredBridge(cfg *config.Config) *ODataMCPBridge {
	cfg.UniversalTool = true

	return &ODataMCPBridge{
		config:      cfg,
		tools:       make(map[string]*models.ToolInfo),
		hintManager: hint.NewManager(),
		metadata: &models.ODataMetadata{
			EntitySets: map[string]*models.EntitySet{
				"People":   {Name: "People", EntityType: "Person", Creatable: true, Updatable: true, Deletable: true},
				"Salaries": {Name: "Salaries", EntityType: "Salary", Creatable: true, Updatable: true, Deletable: true},
			},
			EntityTypes: map[string]*models.EntityType{
				"Person": {Name: "Person", KeyProperties: []string{"PersonCode"}},
				"Salary": {Name: "Salary", KeyProperties: []string{"ID"}},
			},
			FunctionImports: map[string]*models.FunctionImport{
				"Recalculate": {Name: "Recalculate", HTTPMethod: "POST"},
				"Report":      {Name: "Report", HTTPMethod: "GET"},
			},
		},
	}
}

func callUniversal(t *testing.T, b *ODataMCPBridge, action, target string) error {
	t.Helper()

	_, err := b.handleUniversalTool(context.Background(), map[string]any{
		"action": action,
		"target": target,
		"params": map[string]any{"key": map[string]any{"PersonCode": "X", "ID": "1"}, "data": map[string]any{}},
	})

	return err
}

func TestUniversalToolRefusesAnEntityOutsideTheFilter(t *testing.T) {
	b := filteredBridge(&config.Config{AllowedEntities: []string{"People"}})

	for _, action := range []string{"list", "get", "count", "create", "update", "delete"} {
		err := callUniversal(t, b, action, "Salaries")
		if err == nil || !strings.Contains(err.Error(), "unknown target") {
			t.Errorf("%s Salaries with --entities People: error = %v, want unknown target", action, err)
		}
	}
}

func TestUniversalToolRefusesAFunctionOutsideTheFilter(t *testing.T) {
	b := filteredBridge(&config.Config{AllowedFunctions: []string{"Report"}})

	err := callUniversal(t, b, "call", "Recalculate")
	if err == nil || !strings.Contains(err.Error(), "unknown target") {
		t.Errorf("call Recalculate with --functions Report: error = %v, want unknown target", err)
	}
}

func TestUniversalToolHonoursDisabledOperations(t *testing.T) {
	tests := []struct {
		disable string
		action  string
	}{
		{"d", "delete"},
		{"cud", "create"},
		{"cud", "update"},
		{"a", "call"},
		{"r", "list"},
		{"g", "get"},
	}

	for _, tt := range tests {
		t.Run(tt.disable+"/"+tt.action, func(t *testing.T) {
			b := filteredBridge(&config.Config{DisableOps: tt.disable})

			target := "People"
			if tt.action == "call" {
				target = "Recalculate"
			}

			err := callUniversal(t, b, tt.action, target)
			if err == nil || !strings.Contains(err.Error(), "disabled") {
				t.Errorf("--disable %s then %s: error = %v, want disabled", tt.disable, tt.action, err)
			}
		})
	}
}

func TestUniversalToolWithEnableOnlyReadRefusesWrites(t *testing.T) {
	b := filteredBridge(&config.Config{EnableOps: "r"})

	for _, action := range []string{"create", "update", "delete"} {
		if err := callUniversal(t, b, action, "People"); err == nil || !strings.Contains(err.Error(), "disabled") {
			t.Errorf("--enable r then %s: error = %v, want disabled", action, err)
		}
	}
}
