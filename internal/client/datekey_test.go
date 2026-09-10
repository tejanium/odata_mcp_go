// Copyright (c) 2024 OData MCP Contributors
// SPDX-License-Identifier: MIT

package client

import (
	"testing"

	"github.com/zmcp/odata-mcp/internal/models"
)

func TestDateTimeKeysAreTypedLiterals(t *testing.T) {
	c := &ODataClient{}

	tests := []struct {
		name string
		key  map[string]interface{}
		want string
	}{
		{"full literal as given", map[string]interface{}{"EffectiveFrom": models.DateTimeValue("2000-01-08T00:00:00")}, "datetime'2000-01-08T00:00:00'"},
		{"fraction kept", map[string]interface{}{"EffectiveFrom": models.DateTimeValue("2000-01-08T09:30:00.500")}, "datetime'2000-01-08T09:30:00.500'"},
		{"date only as given", map[string]interface{}{"EffectiveFrom": models.DateTimeValue("2000-01-08")}, "datetime'2000-01-08'"},
		{"quote cannot break out", map[string]interface{}{"At": models.DateTimeValue("2000-01-08')/Other?(")}, "datetime'2000-01-08%27%27%29%2FOther%3F%28'"},
		{"composite sorted with typed date", map[string]interface{}{
			"PersonCode":    "8925",
			"EffectiveFrom": models.DateTimeValue("2000-01-08"),
			"CompItemCode":  "BASE",
		}, "CompItemCode='BASE',EffectiveFrom=datetime'2000-01-08',PersonCode='8925'"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := c.buildKeyPredicate(tt.key); got != tt.want {
				t.Errorf("buildKeyPredicate() = %q, want %q", got, tt.want)
			}
		})
	}
}
