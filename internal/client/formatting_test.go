// Copyright (c) 2024 OData MCP Contributors
// SPDX-License-Identifier: MIT

package client

import (
	"testing"

	"github.com/zmcp/odata-mcp/internal/models"
)

func TestBuildKeyPredicateOrdersCompositePartsByName(t *testing.T) {
	c := &ODataClient{}

	tests := []struct {
		name string
		key  map[string]interface{}
		want string
	}{
		{"two parts", map[string]interface{}{"OrderID": 12345, "ItemID": "ABC"}, "ItemID='ABC',OrderID=12345"},
		{"three parts", map[string]interface{}{"C": 3, "A": 1, "B": 2}, "A=1,B=2,C=3"},
		{"mixed types", map[string]interface{}{"Plant": "0001", "LineNo": 7}, "LineNo=7,Plant='0001'"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := c.buildKeyPredicate(tt.key); got != tt.want {
				t.Errorf("buildKeyPredicate() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestBuildKeyPredicateIsStableAcrossCalls(t *testing.T) {
	c := &ODataClient{}

	key := map[string]interface{}{
		"OrderID":  12345,
		"ItemID":   "ABC",
		"LineNo":   7,
		"Revision": "R2",
		"Plant":    "0001",
	}

	want := "ItemID='ABC',LineNo=7,OrderID=12345,Plant='0001',Revision='R2'"

	// Map order varies per range, so one comparison could pass by luck.
	for i := 0; i < 200; i++ {
		if got := c.buildKeyPredicate(key); got != want {
			t.Fatalf("buildKeyPredicate() = %q on call %d, want %q every time", got, i, want)
		}
	}
}

func TestFormatKeyValueKeepsStringsInsideThePredicate(t *testing.T) {
	c := &ODataClient{}

	tests := []struct {
		name  string
		value interface{}
		want  string
	}{
		{"plain code", "ZZ01", "'ZZ01'"},
		{"apostrophe doubled and escaped", "O'Brien", "'O%27%27Brien'"},
		{"space", "ZZ 01", "'ZZ%2001'"},
		{"attempt to leave the predicate", "x')/Salaries?$top=1&(", "'x%27%27%29%2FSalaries%3F$top=1&%28'"},
		{"guid untouched", models.GUIDValue("0F1E2D3C-4B5A-6978-8796-A5B4C3D2E1F0"), "guid'0F1E2D3C-4B5A-6978-8796-A5B4C3D2E1F0'"},
		{"integer untouched", 42, "42"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := c.formatKeyValue(tt.value); got != tt.want {
				t.Errorf("formatKeyValue(%v) = %q, want %q", tt.value, got, tt.want)
			}
		})
	}
}
