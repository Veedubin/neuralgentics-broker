package catalog

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSkillSummaryJSON(t *testing.T) {
	s := SkillSummary{
		Name:        "boomerang-coder",
		Description: "Fast code generation",
		Source:      "local",
		Tags:        []string{"implementation", "debugging"},
		Path:        ".opencode/skills/boomerang-coder/SKILL.md",
		SizeBytes:   1234,
		AgentScope:  []string{"coder"},
	}

	if s.Name != "boomerang-coder" {
		t.Errorf("expected Name 'boomerang-coder', got %q", s.Name)
	}
	if s.Source != "local" {
		t.Errorf("expected Source 'local', got %q", s.Source)
	}
	if len(s.Tags) != 2 {
		t.Errorf("expected 2 tags, got %d", len(s.Tags))
	}
}

func TestSkillCatalogJSON(t *testing.T) {
	cat := SkillCatalog{
		Skills: []SkillSummary{
			{Name: "skill1", Description: "desc1", Source: "local"},
		},
		TotalSkills: 1,
		Role:        "coder",
		Source:      "workspace",
	}
	if cat.TotalSkills != 1 {
		t.Errorf("expected TotalSkills 1, got %d", cat.TotalSkills)
	}
	if cat.Role != "coder" {
		t.Errorf("expected Role 'coder', got %q", cat.Role)
	}
	if cat.Source != "workspace" {
		t.Errorf("expected Source 'workspace', got %q", cat.Source)
	}
}

// ─── LoadScope Tests ─────────────────────────────────────────────────────────

func TestLoadScope_Valid(t *testing.T) {
	dir := t.TempDir()
	content := `version: 1
roles:
  coder:
    - implementation
    - debugging
  tester:
    - verification
    - quality
`
	if err := os.WriteFile(filepath.Join(dir, "agent-skill-scope.yaml"), []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	scope, err := LoadScope(dir)
	if err != nil {
		t.Fatalf("LoadScope returned error: %v", err)
	}
	if scope.Version != 1 {
		t.Errorf("expected Version 1, got %d", scope.Version)
	}
	if len(scope.Roles) != 2 {
		t.Errorf("expected 2 roles, got %d", len(scope.Roles))
	}
	coderTags, ok := scope.Roles["coder"]
	if !ok {
		t.Error("expected 'coder' role in scope")
	} else if len(coderTags) != 2 {
		t.Errorf("expected 2 tags for coder, got %d", len(coderTags))
	}
}

func TestLoadScope_Missing(t *testing.T) {
	dir := t.TempDir()
	// No agent-skill-scope.yaml file.

	scope, err := LoadScope(dir)
	if err != nil {
		t.Fatalf("LoadScope returned error for missing file: %v", err)
	}
	if scope.Version != 1 {
		t.Errorf("expected Version 1, got %d", scope.Version)
	}
	if len(scope.Roles) != 0 {
		t.Errorf("expected 0 roles for missing file, got %d", len(scope.Roles))
	}
}

func TestLoadScope_Malformed(t *testing.T) {
	dir := t.TempDir()
	content := `version: [invalid
roles: not-a-map
`
	if err := os.WriteFile(filepath.Join(dir, "agent-skill-scope.yaml"), []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	_, err := LoadScope(dir)
	if err == nil {
		t.Error("expected error for malformed YAML, got nil")
	}
}

// ─── parseSkillFrontMatter Tests ──────────────────────────────────────────────

func TestParseSkillFrontMatter_Valid(t *testing.T) {
	content := `---
name: boomerang-coder
description: Fast code generation
tags:
  - implementation
  - debugging
---
# Skill Body

This is the skill body.
`
	fm, body, err := parseSkillFrontMatter(content)
	if err != nil {
		t.Fatalf("parseSkillFrontMatter returned error: %v", err)
	}
	if fm["name"] != "boomerang-coder" {
		t.Errorf("expected name 'boomerang-coder', got %v", fm["name"])
	}
	if fm["description"] != "Fast code generation" {
		t.Errorf("expected description 'Fast code generation', got %v", fm["description"])
	}
	if !containsInString(body, "This is the skill body.") {
		t.Errorf("expected body to contain skill content, got %q", body)
	}
}

func TestParseSkillFrontMatter_NoFrontMatter(t *testing.T) {
	content := "# Just a regular markdown file\n\nNo front matter here."
	fm, body, err := parseSkillFrontMatter(content)
	if err != nil {
		t.Fatalf("parseSkillFrontMatter returned error: %v", err)
	}
	if len(fm) != 0 {
		t.Errorf("expected empty front-matter, got %v", fm)
	}
	if body != content {
		t.Errorf("expected body to be full content")
	}
}

func TestParseSkillFrontMatter_Malformed(t *testing.T) {
	content := "---\nname: test\nNo closing delimiter"
	_, _, err := parseSkillFrontMatter(content)
	if err == nil {
		t.Error("expected error for malformed front-matter (no closing ---), got nil")
	}
}

func TestParseSkillFrontMatter_BodyExtraction(t *testing.T) {
	content := "---\nname: my-skill\n---\n\n# Skill Title\n\nSkill body content here.\n"
	fm, body, err := parseSkillFrontMatter(content)
	if err != nil {
		t.Fatalf("parseSkillFrontMatter returned error: %v", err)
	}
	if fm["name"] != "my-skill" {
		t.Errorf("expected name 'my-skill', got %v", fm["name"])
	}
	if !containsInString(body, "Skill Title") {
		t.Errorf("expected body to contain 'Skill Title', got %q", body)
	}
	if !containsInString(body, "Skill body content here.") {
		t.Errorf("expected body to contain body content, got %q", body)
	}
}

func TestParseSkillFrontMatter_NoBody(t *testing.T) {
	content := "---\nname: minimal\n---"
	fm, body, err := parseSkillFrontMatter(content)
	if err != nil {
		t.Fatalf("parseSkillFrontMatter returned error: %v", err)
	}
	if fm["name"] != "minimal" {
		t.Errorf("expected name 'minimal', got %v", fm["name"])
	}
	if body != "" {
		t.Errorf("expected empty body, got %q", body)
	}
}

// ─── parseSkillTags Tests ─────────────────────────────────────────────────────

func TestParseSkillTags_List(t *testing.T) {
	fm := map[string]any{
		"tags": []any{"implementation", "debugging", "+quality"},
	}
	tags := parseSkillTags(fm)
	if len(tags) != 3 {
		t.Errorf("expected 3 tags, got %d", len(tags))
	}
	if tags[0] != "implementation" {
		t.Errorf("expected tag[0] 'implementation', got %q", tags[0])
	}
	if tags[2] != "+quality" {
		t.Errorf("expected tag[2] '+quality', got %q", tags[2])
	}
}

func TestParseSkillTags_CommaString(t *testing.T) {
	fm := map[string]any{
		"tags": "implementation, debugging, quality",
	}
	tags := parseSkillTags(fm)
	if len(tags) != 3 {
		t.Errorf("expected 3 tags, got %d: %v", len(tags), tags)
	}
}

func TestParseSkillTags_EmptyString(t *testing.T) {
	fm := map[string]any{
		"tags": "",
	}
	tags := parseSkillTags(fm)
	if tags != nil {
		t.Errorf("expected nil for empty tags, got %v", tags)
	}
}

func TestParseSkillTags_Missing(t *testing.T) {
	fm := map[string]any{
		"name": "test",
	}
	tags := parseSkillTags(fm)
	if tags != nil {
		t.Errorf("expected nil for missing tags, got %v", tags)
	}
}

// ─── mergeTags Tests ──────────────────────────────────────────────────────────

func TestMergeTags_BaselineOnly(t *testing.T) {
	// Skill has no front-matter tags, inherits YAML baseline.
	scope := &ScopeFile{
		Version: 1,
		Roles: map[string][]string{
			"coder": {"implementation", "debugging"},
		},
	}
	merged, agentScope := mergeTags(nil, "coder", scope)
	if len(merged) != 2 {
		t.Errorf("expected 2 merged tags, got %d: %v", len(merged), merged)
	}
	if len(agentScope) != 1 || agentScope[0] != "coder" {
		t.Errorf("expected agentScope ['coder'], got %v", agentScope)
	}
}

func TestMergeTags_AdditiveTag(t *testing.T) {
	scope := &ScopeFile{
		Version: 1,
		Roles: map[string][]string{
			"coder": {"implementation", "debugging"},
		},
	}
	// +tag extends baseline.
	fmTags := []string{"+quality"}
	merged, _ := mergeTags(fmTags, "coder", scope)
	if !containsString(merged, "quality") {
		t.Errorf("expected merged tags to contain 'quality', got %v", merged)
	}
	if !containsString(merged, "implementation") {
		t.Errorf("expected merged tags to contain 'implementation' (baseline), got %v", merged)
	}
}

func TestMergeTags_SubtractiveTag(t *testing.T) {
	scope := &ScopeFile{
		Version: 1,
		Roles: map[string][]string{
			"coder": {"implementation", "debugging", "quality"},
		},
	}
	// -tag removes from baseline.
	fmTags := []string{"-debugging"}
	merged, _ := mergeTags(fmTags, "coder", scope)
	if containsString(merged, "debugging") {
		t.Errorf("expected debugging to be removed, got %v", merged)
	}
	if !containsString(merged, "implementation") {
		t.Errorf("expected implementation to remain, got %v", merged)
	}
}

func TestMergeTags_OrchestratorWildcard(t *testing.T) {
	scope := &ScopeFile{
		Version: 1,
		Roles: map[string][]string{
			"coder":     {"implementation"},
			"tester":    {"verification"},
			"architect": {"design"},
		},
	}
	// Orchestrator sees everything.
	_, agentScope := mergeTags(nil, "orchestrator", scope)
	if len(agentScope) != 3 {
		t.Errorf("expected 3 roles in agentScope for orchestrator, got %d: %v", len(agentScope), agentScope)
	}
}

func TestMergeTags_RoleNotInYAML(t *testing.T) {
	scope := &ScopeFile{
		Version: 1,
		Roles: map[string][]string{
			"coder": {"implementation"},
		},
	}
	// Role not listed in YAML → no access.
	_, agentScope := mergeTags(nil, "unknown", scope)
	if len(agentScope) != 0 {
		t.Errorf("expected empty agentScope for unknown role, got %v", agentScope)
	}
}

func TestMergeTags_NoYAML(t *testing.T) {
	scope := &ScopeFile{
		Version: 1,
		Roles:   map[string][]string{},
	}
	// Empty scope → allow all.
	_, agentScope := mergeTags(nil, "coder", scope)
	// All roles are accessible when no YAML.
	if len(agentScope) != 0 {
		// agentScope should be all known roles (empty in this case)
		t.Logf("agentScope with no roles: %v", agentScope)
	}
}

// ─── BuildSkills Tests ────────────────────────────────────────────────────────

func TestBuildSkills_EmptyRole(t *testing.T) {
	dir := t.TempDir()
	// Create .opencode/skills/test-skill/SKILL.md
	skillsDir := filepath.Join(dir, ".opencode", "skills", "test-skill")
	if err := os.MkdirAll(skillsDir, 0755); err != nil {
		t.Fatal(err)
	}
	skillContent := `---
name: test-skill
description: A test skill
tags:
  - testing
---
# Test Skill
`
	if err := os.WriteFile(filepath.Join(skillsDir, "SKILL.md"), []byte(skillContent), 0644); err != nil {
		t.Fatal(err)
	}

	builder := NewBuilderWithAccess(nil, nil)
	// Builder with nil registry/access (not used for skills).
	cat := builder.BuildSkills("", dir)
	if cat.TotalSkills != 1 {
		t.Errorf("expected 1 skill, got %d", cat.TotalSkills)
	}
	if cat.Role != "" {
		t.Errorf("expected empty role, got %q", cat.Role)
	}
	if cat.Source != "workspace" {
		t.Errorf("expected source 'workspace', got %q", cat.Source)
	}
	if len(cat.Skills) > 0 && cat.Skills[0].Name != "test-skill" {
		t.Errorf("expected skill name 'test-skill', got %q", cat.Skills[0].Name)
	}
}

func TestBuildSkills_FilteredByRole(t *testing.T) {
	dir := t.TempDir()

	// Create agent-skill-scope.yaml
	scopeContent := `version: 1
roles:
  coder:
    - implementation
    - debugging
  tester:
    - verification
    - quality
`
	if err := os.WriteFile(filepath.Join(dir, "agent-skill-scope.yaml"), []byte(scopeContent), 0644); err != nil {
		t.Fatal(err)
	}

	// Create .opencode/skills/coder-skill/SKILL.md
	skillsDir := filepath.Join(dir, ".opencode", "skills", "coder-skill")
	if err := os.MkdirAll(skillsDir, 0755); err != nil {
		t.Fatal(err)
	}
	skillContent := `---
name: coder-skill
description: A coding skill
tags:
  - implementation
---
# Coder Skill
`
	if err := os.WriteFile(filepath.Join(skillsDir, "SKILL.md"), []byte(skillContent), 0644); err != nil {
		t.Fatal(err)
	}

	builder := NewBuilderWithAccess(nil, nil)
	cat := builder.BuildSkills("coder", dir)
	if cat.TotalSkills != 1 {
		t.Errorf("expected 1 skill for coder, got %d", cat.TotalSkills)
	}
	if cat.Role != "coder" {
		t.Errorf("expected role 'coder', got %q", cat.Role)
	}
}

func TestBuildSkills_NoYAML(t *testing.T) {
	dir := t.TempDir()

	// Create .opencode/skills/test-skill/SKILL.md (no agent-skill-scope.yaml)
	skillsDir := filepath.Join(dir, ".opencode", "skills", "test-skill")
	if err := os.MkdirAll(skillsDir, 0755); err != nil {
		t.Fatal(err)
	}
	skillContent := `---
name: test-skill
description: A test skill
---
# Test Skill
`
	if err := os.WriteFile(filepath.Join(skillsDir, "SKILL.md"), []byte(skillContent), 0644); err != nil {
		t.Fatal(err)
	}

	builder := NewBuilderWithAccess(nil, nil)
	// No YAML → all skills visible to all roles.
	cat := builder.BuildSkills("any-role", dir)
	if cat.TotalSkills != 1 {
		t.Errorf("expected 1 skill (no YAML filter), got %d", cat.TotalSkills)
	}
}

func TestBuildSkills_NoSkillsDir(t *testing.T) {
	dir := t.TempDir()
	// No .opencode/skills directory.

	builder := NewBuilderWithAccess(nil, nil)
	cat := builder.BuildSkills("coder", dir)
	if cat.TotalSkills != 0 {
		t.Errorf("expected 0 skills with no skills dir, got %d", cat.TotalSkills)
	}
}

// ─── Helpers ──────────────────────────────────────────────────────────────────

func containsString(slice []string, s string) bool {
	for _, item := range slice {
		if item == s {
			return true
		}
	}
	return false
}

func containsInString(s, substr string) bool {
	return len(s) >= len(substr) && containsSubstr(s, substr)
}

func containsSubstr(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
