// Copyright (c) 2024 OData MCP Contributors
// SPDX-License-Identifier: MIT

package hint

import (
	"sync"
	"testing"
)

func TestMatchesPattern(t *testing.T) {
	tests := []struct {
		name    string
		url     string
		pattern string
		want    bool
	}{
		{
			name:    "exact match",
			url:     "https://example.com/svc/",
			pattern: "https://example.com/svc/",
			want:    true,
		},
		{
			name:    "catch-all",
			url:     "https://example.com/svc/",
			pattern: "*",
			want:    true,
		},
		{
			name:    "leading and trailing wildcard",
			url:     "https://example.com/svc/",
			pattern: "*example*",
			want:    true,
		},
		{
			name:    "trailing wildcard after a dotted host",
			url:     "https://example.com/svc/",
			pattern: "https://example.com/*",
			want:    true,
		},
		{
			name:    "dotted host in the middle",
			url:     "https://showcase.hr-vendor.com/tenant/dataservice.svc/",
			pattern: "*.hr-vendor.com/*",
			want:    true,
		},
		{
			name:    "dotted service document suffix",
			url:     "https://example.com/tenant/dataservice.svc/",
			pattern: "*/dataservice.svc/",
			want:    true,
		},
		{
			name:    "the shipped SAP pattern still matches",
			url:     "https://sap.example.com/sap/opu/odata/sap/SERVICE/",
			pattern: "*/sap/opu/odata/*",
			want:    true,
		},
		{
			name:    "the shipped Northwind pattern still matches",
			url:     "https://services.odata.org/V2/Northwind/Northwind.svc/",
			pattern: "*Northwind*",
			want:    true,
		},
		{
			name:    "a different host does not match",
			url:     "https://other.example.org/svc/",
			pattern: "https://example.com/*",
			want:    false,
		},
		{
			name:    "a dot is not a wildcard",
			url:     "https://exampleXcom/svc/",
			pattern: "https://example.com/*",
			want:    false,
		},
		{
			name:    "question mark matches exactly one character",
			url:     "https://example.com/v7/svc/",
			pattern: "*/v?/svc/",
			want:    true,
		},
		{
			name:    "question mark does not match two characters",
			url:     "https://example.com/v77/svc/",
			pattern: "*/v?/svc/",
			want:    false,
		},
		{
			name:    "question mark does not match zero characters",
			url:     "https://example.com/v/svc/",
			pattern: "*/v?/svc/",
			want:    false,
		},
		{
			name:    "anchored at the end without a trailing wildcard",
			url:     "https://example.com/svc/extra",
			pattern: "https://example.com/svc/",
			want:    false,
		},
		{
			name:    "anchored at the start without a leading wildcard",
			url:     "prefix-https://example.com/svc/",
			pattern: "https://example.com/*",
			want:    false,
		},
		{
			name:    "regex metacharacters are literal",
			url:     "https://example.com/svc(1)/[a]+b/",
			pattern: "*/svc(1)/[a]+b/",
			want:    true,
		},
		{
			name:    "a regex that would match is not honoured as a regex",
			url:     "https://example.com/svc/",
			pattern: ".*",
			want:    false,
		},
		{
			name:    "empty pattern matches only an empty URL",
			url:     "https://example.com/",
			pattern: "",
			want:    false,
		},
		{
			name:    "consecutive wildcards behave as one",
			url:     "https://example.com/svc/",
			pattern: "*example**svc*",
			want:    true,
		},
		{
			name:    "matching is case sensitive",
			url:     "https://EXAMPLE.com/svc/",
			pattern: "https://example.com/*",
			want:    false,
		},
	}

	m := NewManager()

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := m.matchesPattern(tt.url, tt.pattern); got != tt.want {
				t.Errorf("matchesPattern(%q, %q) = %v, want %v", tt.url, tt.pattern, got, tt.want)
			}
		})
	}
}

func TestMatchesPatternSelectsHintsByDottedHost(t *testing.T) {
	m := managerWith(
		ServiceHint{Pattern: "*", Priority: 1, ServiceType: "Generic OData"},
		ServiceHint{Pattern: "*.hr-vendor.com/*", Priority: 50, ServiceType: "HR Vendor"},
	)

	hints := m.GetHints("https://showcase.hr-vendor.com/tenant/dataservice.svc/")
	if got := hints["service_type"]; got != "HR Vendor" {
		t.Errorf("service_type = %v, want the dotted-host hint to win", got)
	}

	hints = m.GetHints("https://sap.example.org/sap/opu/odata/svc/")
	if got := hints["service_type"]; got != "Generic OData" {
		t.Errorf("service_type = %v, want only the catch-all to match", got)
	}
}

func TestCompileWildcardPatternIsCached(t *testing.T) {
	first, err := compileWildcardPattern("*/cached.svc/*")
	if err != nil {
		t.Fatalf("compileWildcardPattern() error = %v", err)
	}

	second, err := compileWildcardPattern("*/cached.svc/*")
	if err != nil {
		t.Fatalf("compileWildcardPattern() error = %v", err)
	}

	if first != second {
		t.Error("compileWildcardPattern() recompiled an identical pattern")
	}
}

func TestMatchesPatternIsConcurrencySafe(t *testing.T) {
	m := NewManager()
	patterns := []string{"*", "*/svc/*", "https://example.com/*", "*.hr-vendor.com/*", "*/v?/svc/"}

	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			m.matchesPattern("https://example.com/svc/", patterns[i%len(patterns)])
		}(i)
	}
	wg.Wait()
}
