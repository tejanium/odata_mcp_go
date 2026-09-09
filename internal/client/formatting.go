// Copyright (c) 2024 OData MCP Contributors
// SPDX-License-Identifier: MIT

package client

import (
	"fmt"
	"net/url"
	"sort"
	"strings"

	"github.com/zmcp/odata-mcp/internal/models"
)

// encodeQueryParams encodes URL query parameters with proper space encoding
// OData servers expect spaces to be encoded as %20, not + (RFC 3986)
func encodeQueryParams(params url.Values) string {
	encoded := params.Encode()
	// Replace '+' with '%20' for OData compatibility
	return strings.ReplaceAll(encoded, "+", "%20")
}

// buildKeyPredicate builds OData key predicate from key-value pairs
func (c *ODataClient) buildKeyPredicate(key map[string]interface{}) string {
	if len(key) == 1 {
		// Single key
		for _, value := range key {
			return c.formatKeyValue(value)
		}
	}

	// Sorted so the same key always produces the same URL, which map order
	// alone does not guarantee.
	names := make([]string, 0, len(key))
	for name := range key {
		names = append(names, name)
	}
	sort.Strings(names)

	parts := make([]string, 0, len(names))
	for _, name := range names {
		parts = append(parts, fmt.Sprintf("%s=%s", name, c.formatKeyValue(key[name])))
	}

	return strings.Join(parts, ",")
}

// formatKeyValue formats a key value for OData URL
func (c *ODataClient) formatKeyValue(value interface{}) string {
	switch v := value.(type) {
	case models.GUIDValue:
		// SAP OData requires GUID values to be prefixed: guid'value'
		return fmt.Sprintf("guid'%s'", string(v))
	case string:
		// For key predicates, don't URL encode the value inside quotes
		// URL encoding happens at the full URL level
		return fmt.Sprintf("'%s'", v)
	case int, int32, int64:
		return fmt.Sprintf("%d", v)
	case float32, float64:
		return fmt.Sprintf("%g", v)
	case bool:
		return fmt.Sprintf("%t", v)
	default:
		return fmt.Sprintf("'%s'", fmt.Sprintf("%v", v))
	}
}

// formatFunctionParameter formats a function parameter for OData URL
func (c *ODataClient) formatFunctionParameter(key string, value interface{}) string {
	switch v := value.(type) {
	case string:
		// OData requires string parameters to be single-quoted
		// URL encode the value but not the quotes
		return fmt.Sprintf("%s='%s'", key, url.QueryEscape(v))
	case int, int32, int64:
		return fmt.Sprintf("%s=%d", key, v)
	case float32, float64:
		return fmt.Sprintf("%s=%g", key, v)
	case bool:
		return fmt.Sprintf("%s=%t", key, v)
	default:
		// Default to string representation with quotes
		return fmt.Sprintf("%s='%s'", key, url.QueryEscape(fmt.Sprintf("%v", v)))
	}
}
