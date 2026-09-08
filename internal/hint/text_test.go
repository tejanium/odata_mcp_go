// Copyright (c) 2024 OData MCP Contributors
// SPDX-License-Identifier: MIT

package hint

import (
	"strings"
	"testing"
)

const testServiceURL = "https://example.com/svc/"

func managerWith(hints ...ServiceHint) *Manager {
	m := NewManager()
	m.hints = hints
	return m
}

func TestTextIsEmptyWithoutMatchingHints(t *testing.T) {
	m := managerWith(ServiceHint{
		Pattern:     "*/sap/opu/odata/*",
		ServiceType: "SAP OData Service",
		Notes:       []string{"should not appear"},
	})

	if got := m.Text(testServiceURL); got != "" {
		t.Errorf("Text() = %q, want empty for a non-matching pattern", got)
	}
}

func TestTextRendersEverySection(t *testing.T) {
	m := managerWith(ServiceHint{
		Pattern:     "*",
		ServiceType: "Cezanne HR",
		KnownIssues: []string{"eq on _Search columns is case sensitive"},
		Workarounds: []string{"use substringof, not contains"},
		Notes:       []string{"People carries no email"},
		EntityHints: map[string]EntityHint{
			"AllPeopleSearch": {Description: "the only route by email", Notes: []string{"one flat row"}},
		},
		FieldHints: map[string]FieldHint{
			"ActiveEmployee_Search": {Description: "integer flag", Example: "eq 1"},
		},
		Examples: []Example{
			{Description: "find by email", Query: "$filter=InternalEmail_Search eq 'a@b.com'"},
		},
	})

	got := m.Text(testServiceURL)

	for _, want := range []string{
		"Service type: Cezanne HR",
		"Known issues:",
		"eq on _Search columns is case sensitive",
		"Workarounds:",
		"use substringof, not contains",
		"Notes:",
		"People carries no email",
		"Entity notes:",
		"AllPeopleSearch: the only route by email one flat row",
		"Field notes:",
		"ActiveEmployee_Search: integer flag e.g. eq 1",
		"Examples:",
		"find by email -> $filter=InternalEmail_Search eq 'a@b.com'",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("Text() missing %q; got:\n%s", want, got)
		}
	}
}

func TestTextOmitsSectionsWithNoContent(t *testing.T) {
	m := managerWith(ServiceHint{Pattern: "*", Notes: []string{"just a note"}})

	got := m.Text(testServiceURL)

	if !strings.Contains(got, "Notes:") {
		t.Errorf("Text() = %q, want the notes section", got)
	}
	for _, unwanted := range []string{"Known issues:", "Workarounds:", "Entity notes:", "Field notes:", "Examples:", "Service type:"} {
		if strings.Contains(got, unwanted) {
			t.Errorf("Text() contains an empty %q section:\n%s", unwanted, got)
		}
	}
}

func TestTextDeduplicatesAcrossHints(t *testing.T) {
	m := managerWith(
		ServiceHint{Pattern: "*", Priority: 1, Notes: []string{"shared note", "low priority note"}},
		ServiceHint{Pattern: "*/svc/*", Priority: 10, Notes: []string{"shared note", "high priority note"}},
	)

	got := m.Text(testServiceURL)

	if count := strings.Count(got, "shared note"); count != 1 {
		t.Errorf("shared note appears %d times, want 1:\n%s", count, got)
	}
	for _, want := range []string{"low priority note", "high priority note"} {
		if !strings.Contains(got, want) {
			t.Errorf("Text() missing %q:\n%s", want, got)
		}
	}
}

func TestTextServiceTypeFollowsHighestPriority(t *testing.T) {
	m := managerWith(
		ServiceHint{Pattern: "*", Priority: 1, ServiceType: "Generic OData"},
		ServiceHint{Pattern: "*/svc/*", Priority: 10, ServiceType: "Cezanne HR"},
	)

	if got := m.Text(testServiceURL); !strings.Contains(got, "Service type: Cezanne HR") {
		t.Errorf("Text() = %q, want the higher-priority service type", got)
	}
}

func TestTextIncludesCLIHint(t *testing.T) {
	m := NewManager()
	if err := m.SetCLIHint(`{"notes":["from the command line"]}`); err != nil {
		t.Fatalf("SetCLIHint() error = %v", err)
	}

	if got := m.Text(testServiceURL); !strings.Contains(got, "from the command line") {
		t.Errorf("Text() = %q, want the CLI hint included", got)
	}
}

func TestTextIsStableAcrossCalls(t *testing.T) {
	m := managerWith(ServiceHint{
		Pattern: "*",
		EntityHints: map[string]EntityHint{
			"Zebra":  {Description: "last"},
			"Apple":  {Description: "first"},
			"Middle": {Description: "middle"},
		},
		FieldHints: map[string]FieldHint{
			"Zulu":  {Description: "z"},
			"Alpha": {Description: "a"},
		},
	})

	first := m.Text(testServiceURL)
	for i := 0; i < 20; i++ {
		if got := m.Text(testServiceURL); got != first {
			t.Fatalf("Text() is not stable; call %d differs:\n%s\nvs\n%s", i+2, got, first)
		}
	}

	if strings.Index(first, "Apple") > strings.Index(first, "Zebra") {
		t.Error("entity notes are not sorted by name")
	}
	if strings.Index(first, "Alpha") > strings.Index(first, "Zulu") {
		t.Error("field notes are not sorted by name")
	}
}

func TestGetHintsStillWorksAfterMatchingExtraction(t *testing.T) {
	m := managerWith(
		ServiceHint{Pattern: "*", Priority: 1, ServiceType: "Generic OData", Notes: []string{"low"}},
		ServiceHint{Pattern: "*/svc/*", Priority: 10, ServiceType: "Cezanne HR", Notes: []string{"high"}},
	)

	hints := m.GetHints(testServiceURL)
	if hints == nil {
		t.Fatal("GetHints() = nil, want the matching hints")
	}
	if got := hints["service_type"]; got != "Cezanne HR" {
		t.Errorf("service_type = %v, want the higher-priority value", got)
	}

	notes, ok := hints["notes"].([]string)
	if !ok {
		t.Fatalf("notes = %T, want []string", hints["notes"])
	}
	if len(notes) != 2 {
		t.Errorf("notes = %v, want both hints merged", notes)
	}

	if m.GetHints("https://elsewhere/other/")["service_type"] != "Generic OData" {
		t.Error("the catch-all pattern should still match a different URL")
	}
}
