// Copyright (c) 2024 OData MCP Contributors
// SPDX-License-Identifier: MIT

package bridge

import (
	"testing"

	"github.com/zmcp/odata-mcp/internal/models"
)

func TestWrapKeyValueForTypeFollowsTheDeclaredType(t *testing.T) {
	tests := []struct {
		propType string
		value    any
		want     any
	}{
		{"Edm.Guid", "0F1E2D3C-4B5A-6978-8796-A5B4C3D2E1F0", models.GUIDValue("0F1E2D3C-4B5A-6978-8796-A5B4C3D2E1F0")},
		{"Edm.DateTime", "2000-01-08T00:00:00", models.DateTimeValue("2000-01-08T00:00:00")},
		{"Edm.String", "2000-01-08T00:00:00", "2000-01-08T00:00:00"},
		{"Edm.Int32", float64(7), float64(7)},
		{"Edm.DateTime", float64(7), float64(7)},
	}

	for _, tt := range tests {
		if got := wrapKeyValueForType(tt.value, tt.propType); got != tt.want {
			t.Errorf("wrapKeyValueForType(%v, %s) = %#v, want %#v", tt.value, tt.propType, got, tt.want)
		}
	}
}
