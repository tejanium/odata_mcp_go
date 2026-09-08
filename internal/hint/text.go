// Copyright (c) 2024 OData MCP Contributors
// SPDX-License-Identifier: MIT

package hint

import (
	"fmt"
	"sort"
	"strings"
)

// Text renders the hints matching serviceURL as plain text, for embedding in a
// tool description. Empty when no hint matches.
func (m *Manager) Text(serviceURL string) string {
	hints := m.matchingHints(serviceURL)
	if len(hints) == 0 {
		return ""
	}

	var sb strings.Builder

	if serviceType := lastNonEmptyServiceType(hints); serviceType != "" {
		fmt.Fprintf(&sb, "Service type: %s\n", serviceType)
	}

	writeSection(&sb, "Known issues", collectStrings(hints, func(h ServiceHint) []string {
		return h.KnownIssues
	}))
	writeSection(&sb, "Workarounds", collectStrings(hints, func(h ServiceHint) []string {
		return h.Workarounds
	}))
	writeSection(&sb, "Notes", collectStrings(hints, func(h ServiceHint) []string {
		return h.Notes
	}))
	writeSection(&sb, "Entity notes", entityLines(hints))
	writeSection(&sb, "Field notes", fieldLines(hints))
	writeSection(&sb, "Examples", exampleLines(hints))

	return sb.String()
}

func writeSection(sb *strings.Builder, title string, lines []string) {
	if len(lines) == 0 {
		return
	}

	fmt.Fprintf(sb, "%s:\n", title)
	for _, line := range lines {
		fmt.Fprintf(sb, "  - %s\n", line)
	}
}

// lastNonEmptyServiceType mirrors GetHints, where a higher-priority hint
// overwrites the value as the merge walks from lowest priority upward.
func lastNonEmptyServiceType(hints []ServiceHint) string {
	serviceType := ""
	for i := len(hints) - 1; i >= 0; i-- {
		if hints[i].ServiceType != "" {
			serviceType = hints[i].ServiceType
		}
	}
	return serviceType
}

func collectStrings(hints []ServiceHint, pick func(ServiceHint) []string) []string {
	var collected []string
	seen := make(map[string]bool)

	for i := len(hints) - 1; i >= 0; i-- {
		for _, value := range pick(hints[i]) {
			if value == "" || seen[value] {
				continue
			}
			seen[value] = true
			collected = append(collected, value)
		}
	}
	return collected
}

func entityLines(hints []ServiceHint) []string {
	merged := make(map[string]EntityHint)
	for i := len(hints) - 1; i >= 0; i-- {
		for name, entityHint := range hints[i].EntityHints {
			merged[name] = entityHint
		}
	}

	lines := make([]string, 0, len(merged))
	for _, name := range sortedKeys(merged) {
		entityHint := merged[name]
		parts := []string{entityHint.Description}
		parts = append(parts, entityHint.Notes...)
		parts = append(parts, entityHint.Examples...)
		if summary := joinNonEmpty(parts, " "); summary != "" {
			lines = append(lines, fmt.Sprintf("%s: %s", name, summary))
		}
	}
	return lines
}

func fieldLines(hints []ServiceHint) []string {
	merged := make(map[string]FieldHint)
	for i := len(hints) - 1; i >= 0; i-- {
		for name, fieldHint := range hints[i].FieldHints {
			merged[name] = fieldHint
		}
	}

	lines := make([]string, 0, len(merged))
	for _, name := range sortedKeys(merged) {
		fieldHint := merged[name]
		summary := joinNonEmpty([]string{fieldHint.Description, fieldHint.Format}, " ")
		if fieldHint.Example != "" {
			summary = joinNonEmpty([]string{summary, "e.g. " + fieldHint.Example}, " ")
		}
		if summary != "" {
			lines = append(lines, fmt.Sprintf("%s: %s", name, summary))
		}
	}
	return lines
}

func exampleLines(hints []ServiceHint) []string {
	var lines []string
	for i := len(hints) - 1; i >= 0; i-- {
		for _, example := range hints[i].Examples {
			if line := joinNonEmpty([]string{example.Description, example.Query}, " -> "); line != "" {
				lines = append(lines, line)
			}
		}
	}
	return lines
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func joinNonEmpty(parts []string, sep string) string {
	kept := make([]string, 0, len(parts))
	for _, part := range parts {
		if trimmed := strings.TrimSpace(part); trimmed != "" {
			kept = append(kept, trimmed)
		}
	}
	return strings.Join(kept, sep)
}
