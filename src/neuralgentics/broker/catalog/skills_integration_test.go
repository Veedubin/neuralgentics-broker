// Package catalog — integration tests for SkillCatalog pipeline.
//
// These tests exercise BuildSkills end-to-end with real temp directories,
// SKILL.md files, and agent-skill-scope.yaml files. They verify the full
// pipeline: scope loading → front-matter parsing → tag merging → role filtering.
package catalog

import (
	"os"
	"path/filepath"
	"testing"
)

// ─── TestBuildSkills_EndToEnd ────────────────────────────────────────────────

func TestBuildSkills_EndToEnd(t *testing.T) {
	dir := t.TempDir()

	// Create agent-skill-scope.yaml with 2 roles.
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

	// Create 3 SKILL.md files with different front-matter.
	skills := []struct {
		dirName string
		content string
	}{
		{
			dirName: "coder-skill",
			content: `---
name: coder-skill
description: A coding skill
tags:
  - implementation
---
# Coder Skill
`,
		},
		{
			dirName: "tester-skill",
			content: `---
name: tester-skill
description: A testing skill
tags:
  - verification
---
# Tester Skill
`,
		},
		{
			dirName: "generic-skill",
			content: `---
name: generic-skill
description: A generic skill
tags:
  - general
---
# Generic Skill
`,
		},
	}

	for _, s := range skills {
		skillDir := filepath.Join(dir, ".opencode", "skills", s.dirName)
		if err := os.MkdirAll(skillDir, 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte(s.content), 0644); err != nil {
			t.Fatal(err)
		}
	}

	builder := NewBuilderWithAccess(nil, nil)
	cat := builder.BuildSkills("coder", dir)

	// Coder sees all 3 skills because the YAML baseline tags are always
	// included in the merged set, guaranteeing overlap with the role's YAML tags.
	// - coder-skill: merged=[implementation], overlap with [implementation, debugging] ✓
	// - tester-skill: merged=[implementation, debugging, verification], overlap ✓
	// - generic-skill: merged=[implementation, debugging, general], overlap ✓
	if cat.TotalSkills != 3 {
		t.Fatalf("expected 3 skills for coder (YAML baseline always overlaps), got %d", cat.TotalSkills)
	}
	if cat.Role != "coder" {
		t.Errorf("expected role 'coder', got %q", cat.Role)
	}
	if cat.Source != "workspace" {
		t.Errorf("expected source 'workspace', got %q", cat.Source)
	}

	// Verify tags are merged correctly for coder-skill.
	var coderSkill *SkillSummary
	for i := range cat.Skills {
		if cat.Skills[i].Name == "coder-skill" {
			coderSkill = &cat.Skills[i]
			break
		}
	}
	if coderSkill == nil {
		t.Fatal("expected coder-skill in catalog")
	}
	// Merged tags = YAML baseline [implementation, debugging] + front-matter [implementation]
	// = [implementation, debugging] (implementation deduplicated).
	if len(coderSkill.Tags) != 2 {
		t.Errorf("expected 2 merged tags (baseline + front-matter), got %d: %v", len(coderSkill.Tags), coderSkill.Tags)
	}
	if !containsString(coderSkill.Tags, "implementation") {
		t.Errorf("expected 'implementation' in merged tags, got %v", coderSkill.Tags)
	}
	if !containsString(coderSkill.Tags, "debugging") {
		t.Errorf("expected 'debugging' (from YAML baseline) in merged tags, got %v", coderSkill.Tags)
	}
}

// ─── TestBuildSkills_OrchestratorSeesAll ─────────────────────────────────────

func TestBuildSkills_OrchestratorSeesAll(t *testing.T) {
	dir := t.TempDir()

	scopeContent := `version: 1
roles:
  coder:
    - implementation
  tester:
    - verification
`
	if err := os.WriteFile(filepath.Join(dir, "agent-skill-scope.yaml"), []byte(scopeContent), 0644); err != nil {
		t.Fatal(err)
	}

	// Create 2 skills with different tags.
	skills := []struct {
		dirName string
		content string
	}{
		{
			dirName: "coder-skill",
			content: `---
name: coder-skill
description: A coding skill
tags:
  - implementation
---
`,
		},
		{
			dirName: "tester-skill",
			content: `---
name: tester-skill
description: A testing skill
tags:
  - verification
---
`,
		},
	}

	for _, s := range skills {
		skillDir := filepath.Join(dir, ".opencode", "skills", s.dirName)
		if err := os.MkdirAll(skillDir, 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte(s.content), 0644); err != nil {
			t.Fatal(err)
		}
	}

	builder := NewBuilderWithAccess(nil, nil)
	cat := builder.BuildSkills("orchestrator", dir)

	// Orchestrator sees all skills regardless of scope.
	if cat.TotalSkills != 2 {
		t.Fatalf("expected 2 skills for orchestrator, got %d", cat.TotalSkills)
	}

	names := make(map[string]bool)
	for _, s := range cat.Skills {
		names[s.Name] = true
	}
	if !names["coder-skill"] {
		t.Error("expected orchestrator to see coder-skill")
	}
	if !names["tester-skill"] {
		t.Error("expected orchestrator to see tester-skill")
	}
}

// ─── TestBuildSkills_EmptyScopeAllowsAll ─────────────────────────────────────

func TestBuildSkills_EmptyScopeAllowsAll(t *testing.T) {
	dir := t.TempDir()
	// No agent-skill-scope.yaml file.

	// Create 2 skills.
	skills := []struct {
		dirName string
		content string
	}{
		{
			dirName: "alpha",
			content: `---
name: alpha
description: Alpha skill
tags:
  - alpha-tag
---
`,
		},
		{
			dirName: "beta",
			content: `---
name: beta
description: Beta skill
tags:
  - beta-tag
---
`,
		},
	}

	for _, s := range skills {
		skillDir := filepath.Join(dir, ".opencode", "skills", s.dirName)
		if err := os.MkdirAll(skillDir, 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte(s.content), 0644); err != nil {
			t.Fatal(err)
		}
	}

	builder := NewBuilderWithAccess(nil, nil)
	cat := builder.BuildSkills("any-role", dir)

	// No YAML → all skills visible to all roles.
	if cat.TotalSkills != 2 {
		t.Fatalf("expected 2 skills with no scope file, got %d", cat.TotalSkills)
	}
}

// ─── TestBuildSkills_FrontmatterTagOverride ─────────────────────────────────

func TestBuildSkills_FrontmatterTagOverride(t *testing.T) {
	dir := t.TempDir()

	// YAML baseline: coder has [implementation], tester has [quality].
	scopeContent := `version: 1
roles:
  coder:
    - implementation
  tester:
    - quality
`
	if err := os.WriteFile(filepath.Join(dir, "agent-skill-scope.yaml"), []byte(scopeContent), 0644); err != nil {
		t.Fatal(err)
	}

	// Skill has front-matter tags: [-implementation, quality].
	// The -implementation removes the YAML baseline tag, leaving only [quality].
	// Coder's YAML baseline is [implementation], so merged=[quality] has NO overlap.
	// Tester's YAML baseline is [quality], so merged=[quality] HAS overlap.
	skillContent := `---
name: quality-skill
description: A quality skill
tags:
  - -implementation
  - quality
---
# Quality Skill
`
	skillDir := filepath.Join(dir, ".opencode", "skills", "quality-skill")
	if err := os.MkdirAll(skillDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte(skillContent), 0644); err != nil {
		t.Fatal(err)
	}

	builder := NewBuilderWithAccess(nil, nil)

	// Coder should NOT see this skill (merged=[quality], no overlap with [implementation]).
	catCoder := builder.BuildSkills("coder", dir)
	if catCoder.TotalSkills != 0 {
		t.Errorf("expected 0 skills for coder (no tag overlap after subtractive modifier), got %d", catCoder.TotalSkills)
	}

	// Tester should see this skill (merged=[quality], overlap with tester's [quality]).
	catTester := builder.BuildSkills("tester", dir)
	if catTester.TotalSkills != 1 {
		t.Fatalf("expected 1 skill for tester, got %d", catTester.TotalSkills)
	}
	if catTester.Skills[0].Name != "quality-skill" {
		t.Errorf("expected skill name 'quality-skill', got %q", catTester.Skills[0].Name)
	}
}

// ─── TestBuildSkills_AdditiveTagModifier ─────────────────────────────────────

func TestBuildSkills_AdditiveTagModifier(t *testing.T) {
	dir := t.TempDir()

	// YAML baseline: coder has [implementation].
	scopeContent := `version: 1
roles:
  coder:
    - implementation
  tester:
    - quality
`
	if err := os.WriteFile(filepath.Join(dir, "agent-skill-scope.yaml"), []byte(scopeContent), 0644); err != nil {
		t.Fatal(err)
	}

	// Skill has front-matter tags: [implementation, +quality].
	// The +quality modifier adds quality to the merged set.
	// Coder's baseline is [implementation], so merged = [implementation, quality].
	// Since quality is in the merged set AND in coder's YAML tags (implementation),
	// there IS overlap (implementation), so coder should see it.
	skillContent := `---
name: hybrid-skill
description: A hybrid skill
tags:
  - implementation
  - +quality
---
# Hybrid Skill
`
	skillDir := filepath.Join(dir, ".opencode", "skills", "hybrid-skill")
	if err := os.MkdirAll(skillDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte(skillContent), 0644); err != nil {
		t.Fatal(err)
	}

	builder := NewBuilderWithAccess(nil, nil)
	cat := builder.BuildSkills("coder", dir)

	// Coder should see this skill because merged tags include "implementation"
	// which overlaps with coder's YAML baseline [implementation].
	if cat.TotalSkills != 1 {
		t.Fatalf("expected 1 skill for coder (additive tag), got %d", cat.TotalSkills)
	}
	if cat.Skills[0].Name != "hybrid-skill" {
		t.Errorf("expected skill name 'hybrid-skill', got %q", cat.Skills[0].Name)
	}

	// Verify merged tags include both implementation and quality.
	merged := cat.Skills[0].Tags
	if !containsString(merged, "implementation") {
		t.Errorf("expected merged tags to contain 'implementation', got %v", merged)
	}
	if !containsString(merged, "quality") {
		t.Errorf("expected merged tags to contain 'quality' (from +modifier), got %v", merged)
	}
}

// ─── TestBuildSkills_MissingWorkspace ────────────────────────────────────────

func TestBuildSkills_MissingWorkspace(t *testing.T) {
	builder := NewBuilderWithAccess(nil, nil)
	cat := builder.BuildSkills("coder", "/nonexistent/path/that/does/not/exist")

	// Should return empty catalog, not error.
	if cat.TotalSkills != 0 {
		t.Errorf("expected 0 skills for missing workspace, got %d", cat.TotalSkills)
	}
	if cat.Source != "workspace" {
		t.Errorf("expected source 'workspace', got %q", cat.Source)
	}
	if cat.Role != "coder" {
		t.Errorf("expected role 'coder', got %q", cat.Role)
	}
}

// ─── TestBuildSkills_ParseFrontMatter_Empty ──────────────────────────────────

func TestBuildSkills_ParseFrontMatter_Empty(t *testing.T) {
	// No front-matter at all — just markdown.
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

// ─── TestBuildSkills_ParseFrontMatter_Malformed ──────────────────────────────

func TestBuildSkills_ParseFrontMatter_Malformed(t *testing.T) {
	// Opening --- but no closing --- and bad YAML.
	content := "---\nname: test\ninvalid: [yaml\nNo closing delimiter"
	_, _, err := parseSkillFrontMatter(content)
	if err == nil {
		t.Error("expected error for malformed front-matter (no closing ---), got nil")
	}
}
