// Copyright (c) 2024 OData MCP Contributors
// SPDX-License-Identifier: MIT

package client

import (
	"testing"
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
