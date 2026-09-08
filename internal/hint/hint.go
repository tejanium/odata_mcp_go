package hint

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
)

// patternCache memoises compiled wildcard patterns, which are matched once per
// hint per lookup and never change.
var patternCache sync.Map

// ServiceHint represents hints for a specific service pattern
type ServiceHint struct {
	Pattern       string                  `json:"pattern"`
	Priority      int                     `json:"priority,omitempty"`
	ServiceType   string                  `json:"service_type,omitempty"`
	KnownIssues   []string                `json:"known_issues,omitempty"`
	Workarounds   []string                `json:"workarounds,omitempty"`
	FieldHints    map[string]FieldHint    `json:"field_hints,omitempty"`
	EntityHints   map[string]EntityHint   `json:"entity_hints,omitempty"`
	FunctionHints map[string]FunctionHint `json:"function_hints,omitempty"`
	Examples      []Example               `json:"examples,omitempty"`
	Notes         []string                `json:"notes,omitempty"`
}

// FieldHint provides hints for specific fields
type FieldHint struct {
	Type        string `json:"type,omitempty"`
	Format      string `json:"format,omitempty"`
	Example     string `json:"example,omitempty"`
	Description string `json:"description,omitempty"`
	Required    bool   `json:"required,omitempty"`
}

// EntityHint provides hints for specific entities
type EntityHint struct {
	Description string   `json:"description,omitempty"`
	Notes       []string `json:"notes,omitempty"`
	Examples    []string `json:"examples,omitempty"`
}

// FunctionHint provides hints for specific functions
type FunctionHint struct {
	Description string   `json:"description,omitempty"`
	Parameters  []string `json:"parameters,omitempty"`
	Examples    []string `json:"examples,omitempty"`
}

// Example represents a usage example
type Example struct {
	Description string `json:"description"`
	Query       string `json:"query"`
	Note        string `json:"note,omitempty"`
}

// HintConfig represents the full hint configuration
type HintConfig struct {
	Version string        `json:"version"`
	Hints   []ServiceHint `json:"hints"`
}

// Manager manages service hints
type Manager struct {
	hints     []ServiceHint
	cliHint   *ServiceHint // Direct hint from CLI
	hintsFile string
}

// NewManager creates a new hint manager
func NewManager() *Manager {
	return &Manager{
		hints: make([]ServiceHint, 0),
	}
}

// LoadFromFile loads hints from a JSON file
func (m *Manager) LoadFromFile(path string) error {
	// If no path provided, try default locations
	if path == "" {
		// Try same directory as binary
		exe, err := os.Executable()
		if err == nil {
			defaultPath := filepath.Join(filepath.Dir(exe), "hints.json")
			if _, err := os.Stat(defaultPath); err == nil {
				path = defaultPath
			}
		}

		// If still no path, try current directory
		if path == "" {
			if _, err := os.Stat("hints.json"); err == nil {
				path = "hints.json"
			}
		}

		// No hints file found is not an error
		if path == "" {
			return nil
		}
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("failed to read hints file: %w", err)
	}

	var config HintConfig
	if err := json.Unmarshal(data, &config); err != nil {
		return fmt.Errorf("failed to parse hints file: %w", err)
	}

	m.hints = config.Hints
	m.hintsFile = path

	return nil
}

// SetCLIHint sets a hint directly from command line
func (m *Manager) SetCLIHint(hintJSON string) error {
	var hint ServiceHint
	if err := json.Unmarshal([]byte(hintJSON), &hint); err != nil {
		// Try as simple string hint
		hint = ServiceHint{
			Pattern: "*",
			Notes:   []string{hintJSON},
		}
	}

	// CLI hints have highest priority
	hint.Priority = 1000
	m.cliHint = &hint

	return nil
}

// GetHints returns all matching hints for a service URL
func (m *Manager) GetHints(serviceURL string) map[string]interface{} {
	matchingHints := m.matchingHints(serviceURL)

	// No hints found
	if len(matchingHints) == 0 {
		return nil
	}

	// Merge hints (higher priority overrides)
	result := make(map[string]interface{})

	// Start from lowest priority and work up
	for i := len(matchingHints) - 1; i >= 0; i-- {
		hint := matchingHints[i]

		if hint.ServiceType != "" {
			result["service_type"] = hint.ServiceType
		}

		if len(hint.KnownIssues) > 0 {
			existing, _ := result["known_issues"].([]string)
			result["known_issues"] = m.mergeStringSlices(existing, hint.KnownIssues)
		}

		if len(hint.Workarounds) > 0 {
			existing, _ := result["workarounds"].([]string)
			result["workarounds"] = m.mergeStringSlices(existing, hint.Workarounds)
		}

		if len(hint.Notes) > 0 {
			existing, _ := result["notes"].([]string)
			result["notes"] = m.mergeStringSlices(existing, hint.Notes)
		}

		if len(hint.FieldHints) > 0 {
			existing, _ := result["field_hints"].(map[string]interface{})
			if existing == nil {
				existing = make(map[string]interface{})
			}
			for k, v := range hint.FieldHints {
				existing[k] = m.fieldHintToMap(v)
			}
			result["field_hints"] = existing
		}

		if len(hint.EntityHints) > 0 {
			existing, _ := result["entity_hints"].(map[string]interface{})
			if existing == nil {
				existing = make(map[string]interface{})
			}
			for k, v := range hint.EntityHints {
				existing[k] = m.entityHintToMap(v)
			}
			result["entity_hints"] = existing
		}

		if len(hint.FunctionHints) > 0 {
			existing, _ := result["function_hints"].(map[string]interface{})
			if existing == nil {
				existing = make(map[string]interface{})
			}
			for k, v := range hint.FunctionHints {
				existing[k] = m.functionHintToMap(v)
			}
			result["function_hints"] = existing
		}

		if len(hint.Examples) > 0 {
			existing, _ := result["examples"].([]interface{})
			for _, ex := range hint.Examples {
				existing = append(existing, m.exampleToMap(ex))
			}
			result["examples"] = existing
		}
	}

	// Add hint source info
	if m.cliHint != nil {
		result["hint_source"] = "CLI argument"
	} else if m.hintsFile != "" {
		result["hint_source"] = fmt.Sprintf("Hints file: %s", m.hintsFile)
	}

	return result
}

// matchingHints returns the hints matching serviceURL, highest priority first.
func (m *Manager) matchingHints(serviceURL string) []ServiceHint {
	var matchingHints []ServiceHint

	// Add CLI hint if present
	if m.cliHint != nil {
		matchingHints = append(matchingHints, *m.cliHint)
	}

	// Find all matching hints
	for _, hint := range m.hints {
		if m.matchesPattern(serviceURL, hint.Pattern) {
			matchingHints = append(matchingHints, hint)
		}
	}

	// Sort by priority (higher first)
	for i := 0; i < len(matchingHints)-1; i++ {
		for j := i + 1; j < len(matchingHints); j++ {
			if matchingHints[j].Priority > matchingHints[i].Priority {
				matchingHints[i], matchingHints[j] = matchingHints[j], matchingHints[i]
			}
		}
	}

	return matchingHints
}

// matchesPattern checks if a URL matches a pattern with wildcards,
// where * is any sequence of characters and ? is a single character.
func (m *Manager) matchesPattern(url, pattern string) bool {
	// Direct match
	if url == pattern {
		return true
	}

	compiled, err := compileWildcardPattern(pattern)
	if err != nil {
		return false
	}

	return compiled.MatchString(url)
}

// compileWildcardPattern builds an anchored regexp from a wildcard pattern.
// Everything but * and ? is quoted, so a pattern may contain a dot or a slash.
func compileWildcardPattern(pattern string) (*regexp.Regexp, error) {
	if cached, ok := patternCache.Load(pattern); ok {
		return cached.(*regexp.Regexp), nil
	}

	var expression strings.Builder
	expression.WriteString("^")

	for i, anySequence := range strings.Split(pattern, "*") {
		if i > 0 {
			expression.WriteString(".*")
		}
		for j, literal := range strings.Split(anySequence, "?") {
			if j > 0 {
				expression.WriteString(".")
			}
			expression.WriteString(regexp.QuoteMeta(literal))
		}
	}

	expression.WriteString("$")

	compiled, err := regexp.Compile(expression.String())
	if err != nil {
		return nil, err
	}

	patternCache.Store(pattern, compiled)

	return compiled, nil
}

// mergeStringSlices merges two string slices, avoiding duplicates
func (m *Manager) mergeStringSlices(existing, new []string) []string {
	seen := make(map[string]bool)
	result := make([]string, 0)

	for _, s := range existing {
		if !seen[s] {
			seen[s] = true
			result = append(result, s)
		}
	}

	for _, s := range new {
		if !seen[s] {
			seen[s] = true
			result = append(result, s)
		}
	}

	return result
}

// Helper functions to convert structs to maps
func (m *Manager) fieldHintToMap(hint FieldHint) map[string]interface{} {
	result := make(map[string]interface{})
	if hint.Type != "" {
		result["type"] = hint.Type
	}
	if hint.Format != "" {
		result["format"] = hint.Format
	}
	if hint.Example != "" {
		result["example"] = hint.Example
	}
	if hint.Description != "" {
		result["description"] = hint.Description
	}
	if hint.Required {
		result["required"] = true
	}
	return result
}

func (m *Manager) entityHintToMap(hint EntityHint) map[string]interface{} {
	result := make(map[string]interface{})
	if hint.Description != "" {
		result["description"] = hint.Description
	}
	if len(hint.Notes) > 0 {
		result["notes"] = hint.Notes
	}
	if len(hint.Examples) > 0 {
		result["examples"] = hint.Examples
	}
	return result
}

func (m *Manager) functionHintToMap(hint FunctionHint) map[string]interface{} {
	result := make(map[string]interface{})
	if hint.Description != "" {
		result["description"] = hint.Description
	}
	if len(hint.Parameters) > 0 {
		result["parameters"] = hint.Parameters
	}
	if len(hint.Examples) > 0 {
		result["examples"] = hint.Examples
	}
	return result
}

func (m *Manager) exampleToMap(ex Example) map[string]interface{} {
	result := make(map[string]interface{})
	result["description"] = ex.Description
	result["query"] = ex.Query
	if ex.Note != "" {
		result["note"] = ex.Note
	}
	return result
}
