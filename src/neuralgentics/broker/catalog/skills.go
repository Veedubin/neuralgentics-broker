// Package catalog builds and manages server and skill catalogs for the
// Neuralgentics MCP broker. This file implements the SkillCatalog and
// YAML scope-loading for Phase 1 of the Skills Brokering feature (T-SB-004).
package catalog

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// SkillSummary is a lightweight description of a skill for catalog display.
type SkillSummary struct {
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Source      string   `json:"source"` // "local" | "external" (Phase 1: always "local")
	Tags        []string `json:"tags"`   // merged from YAML + front-matter
	Path        string   `json:"path"`   // relative to workspace root
	SizeBytes   int64    `json:"size_bytes"`
	AgentScope  []string `json:"agent_scope"` // roles this skill is visible to (merged)
}

// SkillCatalog is an aggregate view of all available skills, filtered by role.
type SkillCatalog struct {
	Skills      []SkillSummary `json:"skills"`
	TotalSkills int            `json:"total_skills"`
	Role        string         `json:"role"`
	Source      string         `json:"source"` // "workspace" or "default"
}

// ScopeFile represents the parsed agent-skill-scope.yaml file.
type ScopeFile struct {
	Version int                 `yaml:"version"`
	Roles   map[string][]string `yaml:"roles"` // role-name → [list, of, tags]
}

// LoadScope reads and parses agent-skill-scope.yaml from the given directory.
// The path argument should be the directory containing the YAML file.
// Returns an empty ScopeFile (allow-all) if the file does not exist.
func LoadScope(dir string) (*ScopeFile, error) {
	scopePath := filepath.Join(dir, "agent-skill-scope.yaml")
	data, err := os.ReadFile(scopePath)
	if err != nil {
		if os.IsNotExist(err) {
			// File missing → allow-all (no filtering).
			return &ScopeFile{Version: 1, Roles: map[string][]string{}}, nil
		}
		return nil, fmt.Errorf("read scope file %s: %w", scopePath, err)
	}

	var scope ScopeFile
	if err := yaml.Unmarshal(data, &scope); err != nil {
		return nil, fmt.Errorf("parse scope file %s: %w", scopePath, err)
	}

	return &scope, nil
}

// parseSkillFrontMatter extracts the YAML front-matter block from a SKILL.md file.
// The front-matter is delimited by "---\n...\n---" at the top of the file.
// Returns the parsed front-matter map, the body (everything after the closing "---"),
// and any parse error.
func parseSkillFrontMatter(content string) (frontMatter map[string]any, body string, err error) {
	// Skip leading whitespace.
	trimmed := strings.TrimLeft(content, " \t\r\n")

	// If the file doesn't start with "---\n", no front-matter.
	if !strings.HasPrefix(trimmed, "---\n") && !strings.HasPrefix(trimmed, "---\r\n") {
		return map[string]any{}, content, nil
	}

	// Find the opening delimiter end.
	openEnd := strings.Index(trimmed, "\n")
	if openEnd == -1 {
		return map[string]any{}, content, nil
	}
	afterOpen := trimmed[openEnd+1:]

	// Find the closing "---".
	closeIdx := strings.Index(afterOpen, "\n---")
	if closeIdx == -1 {
		// Try at the very end without trailing newline.
		if strings.HasSuffix(afterOpen, "---") {
			fmBlock := afterOpen[:len(afterOpen)-3]
			var fm map[string]any
			if err := yaml.Unmarshal([]byte(fmBlock), &fm); err != nil {
				return nil, content, fmt.Errorf("parse front-matter: %w", err)
			}
			return fm, "", nil
		}
		return nil, content, fmt.Errorf("front-matter opening --- found but no closing ---")
	}

	fmBlock := afterOpen[:closeIdx]
	// Body starts after the closing "---" and its newline.
	// afterOpen[closeIdx:] starts with "\n---", so we need to find the
	// next newline after the closing "---" delimiter.
	closingDelimEnd := closeIdx + len("\n---")
	var bodyContent string
	if closingDelimEnd < len(afterOpen) {
		// There's content after the closing "---".
		// If the next char is a newline, skip it.
		rest := afterOpen[closingDelimEnd:]
		if len(rest) > 0 && rest[0] == '\n' {
			bodyContent = rest[1:]
		} else {
			bodyContent = rest
		}
	}

	var fm map[string]any
	if err := yaml.Unmarshal([]byte(fmBlock), &fm); err != nil {
		return nil, content, fmt.Errorf("parse front-matter: %w", err)
	}

	return fm, bodyContent, nil
}

// parseSkillTags extracts the tags field from front-matter.
// Handles both string (comma-separated) and []string types.
// Supports +tag/-tag modifier syntax per design doc section 3.
func parseSkillTags(fm map[string]any) []string {
	raw, ok := fm["tags"]
	if !ok {
		return nil
	}

	switch v := raw.(type) {
	case []any:
		tags := make([]string, 0, len(v))
		for _, item := range v {
			if s, ok := item.(string); ok {
				tags = append(tags, s)
			}
		}
		return tags
	case string:
		if v == "" {
			return nil
		}
		parts := strings.Split(v, ",")
		tags := make([]string, 0, len(parts))
		for _, p := range parts {
			p = strings.TrimSpace(p)
			if p != "" {
				tags = append(tags, p)
			}
		}
		return tags
	default:
		return nil
	}
}

// mergeTags applies the YAML baseline + front-matter tag merge rule per design doc section 3.5.
// Returns (mergedTags, agentScope).
func mergeTags(fmTags []string, role string, scope *ScopeFile) (mergedTags []string, agentScope []string) {
	// Collect all known roles from the scope.
	allRoles := make([]string, 0, len(scope.Roles))
	for r := range scope.Roles {
		allRoles = append(allRoles, r)
	}

	yamlTags := scope.Roles[role]

	// Step 2: Determine agentScope.
	if scope.Version > 0 && len(scope.Roles) > 0 {
		if role == "orchestrator" || role == "" {
			// Orchestrator wildcard — sees everything.
			agentScope = allRoles
		} else if yamlTags == nil {
			// Role not listed in YAML → no access.
			agentScope = []string{}
		} else {
			agentScope = []string{role}
		}
	} else {
		// No YAML or empty scope → allow all.
		agentScope = allRoles
	}

	// Step 3: If skill has no front-matter tags.
	if len(fmTags) == 0 {
		if yamlTags != nil {
			mergedTags = make([]string, len(yamlTags))
			copy(mergedTags, yamlTags)
		} else {
			mergedTags = []string{}
		}
		return mergedTags, agentScope
	}

	// Step 4: Skill HAS front-matter tags. Apply additive/subtractive modifiers.
	mergedTags = make([]string, 0)
	if yamlTags != nil {
		mergedTags = append(mergedTags, yamlTags...)
	}

	seen := make(map[string]bool)
	for _, t := range mergedTags {
		seen[t] = true
	}

	for _, tag := range fmTags {
		if strings.HasPrefix(tag, "-") {
			// Remove tag from merged set.
			removeTag := tag[1:]
			delete(seen, removeTag)
			// Rebuild mergedTags from seen.
			newMerged := make([]string, 0, len(seen))
			for _, t := range mergedTags {
				if t != removeTag {
					newMerged = append(newMerged, t)
				}
			}
			mergedTags = newMerged
		} else {
			// Add tag (with or without "+" prefix).
			cleanTag := tag
			if strings.HasPrefix(tag, "+") {
				cleanTag = tag[1:]
			}
			if !seen[cleanTag] {
				mergedTags = append(mergedTags, cleanTag)
				seen[cleanTag] = true
			}
		}
	}

	// Step 5: Role filter check.
	if role != "orchestrator" && role != "" && yamlTags != nil {
		overlap := tagIntersection(mergedTags, yamlTags)
		if len(overlap) == 0 {
			agentScope = []string{}
		}
	}

	return mergedTags, agentScope
}

// tagIntersection returns the intersection of two tag lists.
func tagIntersection(a, b []string) []string {
	bSet := make(map[string]bool, len(b))
	for _, t := range b {
		bSet[t] = true
	}
	var result []string
	for _, t := range a {
		if bSet[t] {
			result = append(result, t)
		}
	}
	return result
}

// BuildSkills constructs a SkillCatalog filtered by role.
// workspaceRoot is the absolute path to the project root.
func (b *Builder) BuildSkills(role string, workspaceRoot string) SkillCatalog {
	// Step 1: Load scope.
	scope, err := LoadScope(workspaceRoot)
	if err != nil {
		// Log warning and use empty scope (allow-all).
		fmt.Fprintf(os.Stderr, "[broker] warning: failed to load skill scope: %v\n", err)
		scope = &ScopeFile{Version: 1, Roles: map[string][]string{}}
	}

	var skills []SkillSummary

	// Step 2: Walk .opencode/skills/*/SKILL.md.
	skillsDir := filepath.Join(workspaceRoot, ".opencode", "skills")
	entries, err := os.ReadDir(skillsDir)
	if err != nil {
		// No skills directory → return empty catalog.
		return SkillCatalog{
			Skills:      skills,
			TotalSkills: 0,
			Role:        role,
			Source:      "workspace",
		}
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		// Skip hidden directories.
		if strings.HasPrefix(entry.Name(), ".") {
			continue
		}

		skillPath := filepath.Join(skillsDir, entry.Name(), "SKILL.md")
		info, statErr := os.Stat(skillPath)
		if statErr != nil {
			// SKILL.md doesn't exist in this directory, skip.
			continue
		}

		content, readErr := os.ReadFile(skillPath)
		if readErr != nil {
			continue
		}

		fm, _, parseErr := parseSkillFrontMatter(string(content))
		if parseErr != nil {
			// Log warning and skip this skill.
			fmt.Fprintf(os.Stderr, "[broker] warning: failed to parse front-matter for %s: %v\n", skillPath, parseErr)
			continue
		}

		// Extract name from front-matter; fall back to directory name.
		name := entry.Name()
		if fmName, ok := fm["name"].(string); ok && fmName != "" {
			name = fmName
		}

		// Extract description from front-matter; fall back to "No description".
		description := "No description"
		if fmDesc, ok := fm["description"].(string); ok && fmDesc != "" {
			description = fmDesc
		}

		// Extract tags from front-matter.
		fmTags := parseSkillTags(fm)

		// Compute relative path.
		relPath, relErr := filepath.Rel(workspaceRoot, skillPath)
		if relErr != nil {
			relPath = skillPath
		}

		// Merge tags.
		mergedTags, agentScope := mergeTags(fmTags, role, scope)

		// Apply role filter:
		// - Empty role or "orchestrator" → include all.
		// - Skill with empty agent_scope AND no YAML scope → include (visible to all).
		// - Skill with empty agent_scope when scope is non-empty → excluded (no tag overlap).
		// - Otherwise, agent_scope determines visibility.
		// When scope has no roles (no YAML file), all skills are visible to all roles.
		noScopeFilter := scope.Version > 0 && len(scope.Roles) == 0
		if role != "" && role != "orchestrator" && !noScopeFilter {
			if len(agentScope) == 0 {
				// Role not in scope or no tag overlap → exclude this skill.
				continue
			}
		}

		skill := SkillSummary{
			Name:        name,
			Description: description,
			Source:      "local",
			Tags:        mergedTags,
			Path:        relPath,
			SizeBytes:   info.Size(),
			AgentScope:  agentScope,
		}
		skills = append(skills, skill)
	}

	// TODO: Phase 2 — handle legacy skills/<role>.md files.

	return SkillCatalog{
		Skills:      skills,
		TotalSkills: len(skills),
		Role:        role,
		Source:      "workspace",
	}
}
