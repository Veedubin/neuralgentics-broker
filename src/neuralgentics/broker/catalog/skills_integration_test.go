// Package catalog — integration tests for SkillCatalog pipeline.
//
// These tests exercise BuildSkills end-to-end with real temp directories,
// SKILL.md files, and agent-skill-scope.yaml files. They verify the full
// pipeline: scope loading → front-matter parsing → tag merging → role filtering.
package catalog

import (
	"encoding/json"
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

// ─── T-SB-009: External Skill Tests ─────────────────────────────────────────

// writeManifest writes a MANIFEST.json to the given external dir.
func writeManifest(t *testing.T, externalDir string, repos map[string]ExternalRepoState) {
	t.Helper()
	manifest := ExternalManifest{
		Version:   1,
		UpdatedAt: "2026-06-24T12:00:00Z",
		HomeDir:   externalDir,
		Repos:     repos,
	}
	data, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(externalDir, "MANIFEST.json"), data, 0644); err != nil {
		t.Fatal(err)
	}
}

// TestBuildSkills_ExternalDir_OrchestratorSeesAll verifies that the orchestrator
// sees both local and external skills when externalDir is set.
func TestBuildSkills_ExternalDir_OrchestratorSeesAll(t *testing.T) {
	dir := t.TempDir()
	extDir := t.TempDir()

	// Create a local skill.
	localSkillDir := filepath.Join(dir, ".opencode", "skills", "local-skill")
	if err := os.MkdirAll(localSkillDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(localSkillDir, "SKILL.md"), []byte(`---
name: local-skill
description: A local skill
tags:
  - implementation
---
`), 0644); err != nil {
		t.Fatal(err)
	}

	// Create an external skill inside the repo-named subdirectory.
	repoDir := filepath.Join(extDir, "ai-research-skills")
	extSkillDir := filepath.Join(repoDir, "01-tools", "ext-skill")
	if err := os.MkdirAll(extSkillDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(extSkillDir, "SKILL.md"), []byte(`---
name: ext-skill
description: An external skill
tags:
  - design
---
`), 0644); err != nil {
		t.Fatal(err)
	}

	// Write manifest.
	writeManifest(t, extDir, map[string]ExternalRepoState{
		"ai-research-skills": {
			URL:         "https://github.com/Orchestra-Research/AI-Research-SKILLs.git",
			CommitSHA:   "abc123def456",
			License:     "MIT",
			Attribution: "Copyright 2025 Contributors. Used under MIT License.",
		},
	})

	builder := NewBuilderWithExternal(nil, nil, dir, extDir)
	cat := builder.BuildSkills("orchestrator", dir)

	if cat.TotalSkills < 2 {
		t.Fatalf("expected at least 2 skills (local + external), got %d", cat.TotalSkills)
	}

	names := make(map[string]bool)
	for _, s := range cat.Skills {
		names[s.Name] = true
	}
	if !names["local-skill"] {
		t.Error("expected local-skill in catalog")
	}
	if !names["ext-skill"] {
		t.Error("expected ext-skill in catalog")
	}

	// Verify ext-skill has Source="external" and provenance.
	for _, s := range cat.Skills {
		if s.Name == "ext-skill" {
			if s.Source != "external" {
				t.Errorf("expected ext-skill Source='external', got %q", s.Source)
			}
			if s.ExternalProvenance == nil {
				t.Error("expected ext-skill to have ExternalProvenance, got nil")
			} else if s.ExternalProvenance.Repo != "ai-research-skills" {
				t.Errorf("expected provenance Repo='ai-research-skills', got %q", s.ExternalProvenance.Repo)
			}
		}
		if s.Name == "local-skill" {
			if s.Source != "local" {
				t.Errorf("expected local-skill Source='local', got %q", s.Source)
			}
			if s.ExternalProvenance != nil {
				t.Errorf("expected local-skill ExternalProvenance=nil, got %+v", s.ExternalProvenance)
			}
		}
	}
}

// TestBuildSkills_ExternalDir_RoleFiltering verifies that external skills
// are filtered by role just like local skills.
func TestBuildSkills_ExternalDir_RoleFiltering(t *testing.T) {
	dir := t.TempDir()
	extDir := t.TempDir()

	// Create scope YAML: coder sees [implementation].
	scopeContent := `version: 1
roles:
  coder:
    - implementation
`
	if err := os.WriteFile(filepath.Join(dir, "agent-skill-scope.yaml"), []byte(scopeContent), 0644); err != nil {
		t.Fatal(err)
	}

	// Create external skill with [design] tag only, under ai-research-skills repo.
	repoDir := filepath.Join(extDir, "ai-research-skills")
	extSkillDir := filepath.Join(repoDir, "01-tools", "design-skill")
	if err := os.MkdirAll(extSkillDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(extSkillDir, "SKILL.md"), []byte(`---
name: design-skill
description: A design skill
tags:
  - design
---
`), 0644); err != nil {
		t.Fatal(err)
	}

	writeManifest(t, extDir, map[string]ExternalRepoState{
		"ai-research-skills": {
			URL:         "https://github.com/example/repo.git",
			CommitSHA:   "deadbeef",
			License:     "MIT",
			Attribution: "Test attribution",
		},
	})

	builder := NewBuilderWithExternal(nil, nil, dir, extDir)
	cat := builder.BuildSkills("coder", dir)

	// coder's YAML tags are [implementation]. External skill has [design].
	// After merge: merged=[implementation, design] (baseline + front-matter).
	// Overlap check: coder baseline [implementation] intersects [implementation, design] → YES.
	// So coder SHOULD see the design-skill (because implementation is in both).
	if cat.TotalSkills != 1 {
		t.Fatalf("expected 1 skill for coder (baseline adds implementation tag), got %d", cat.TotalSkills)
	}
}

// TestBuildSkills_ExternalDir_ProvenanceStamped verifies that all provenance
// fields are correctly populated from MANIFEST.json.
func TestBuildSkills_ExternalDir_ProvenanceStamped(t *testing.T) {
	dir := t.TempDir()
	extDir := t.TempDir()

	repoDir := filepath.Join(extDir, "ai-research-skills")
	extSkillDir := filepath.Join(repoDir, "01-tools", "provenance-skill")
	if err := os.MkdirAll(extSkillDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(extSkillDir, "SKILL.md"), []byte(`---
name: provenance-skill
description: Skill with provenance
---
`), 0644); err != nil {
		t.Fatal(err)
	}

	writeManifest(t, extDir, map[string]ExternalRepoState{
		"ai-research-skills": {
			URL:         "https://github.com/Orchestra-Research/AI-Research-SKILLs.git",
			CommitSHA:   "773a529b8c4d1e2f3a5b6c7d8e9f0a1b2c3d4e5f6",
			License:     "MIT",
			Attribution: "Copyright 2025 Claude AI Research Skills Contributors. Used under MIT License.",
		},
	})

	builder := NewBuilderWithExternal(nil, nil, dir, extDir)
	cat := builder.BuildSkills("", dir) // empty role = see all

	if cat.TotalSkills != 1 {
		t.Fatalf("expected 1 skill, got %d", cat.TotalSkills)
	}
	p := cat.Skills[0].ExternalProvenance
	if p == nil {
		t.Fatal("expected ExternalProvenance, got nil")
	}
	if p.Repo != "ai-research-skills" {
		t.Errorf("expected Repo='ai-research-skills', got %q", p.Repo)
	}
	if p.CommitSHA != "773a529b8c4d1e2f3a5b6c7d8e9f0a1b2c3d4e5f6" {
		t.Errorf("expected CommitSHA, got %q", p.CommitSHA)
	}
	if p.License != "MIT" {
		t.Errorf("expected License='MIT', got %q", p.License)
	}
	if p.Attribution != "Copyright 2025 Claude AI Research Skills Contributors. Used under MIT License." {
		t.Errorf("expected Attribution, got %q", p.Attribution)
	}
}

// TestBuildSkills_ExternalDir_DedupPrefersLocal verifies that a local skill
// with the same name as an external skill wins the dedup.
func TestBuildSkills_ExternalDir_DedupPrefersLocal(t *testing.T) {
	dir := t.TempDir()
	extDir := t.TempDir()

	// Local skill named "test-skill".
	localSkillDir := filepath.Join(dir, ".opencode", "skills", "test-skill")
	if err := os.MkdirAll(localSkillDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(localSkillDir, "SKILL.md"), []byte(`---
name: test-skill
description: Local version
tags:
  - implementation
---
`), 0644); err != nil {
		t.Fatal(err)
	}

	// External skill also named "test-skill", under ai-research-skills repo.
	repoDir := filepath.Join(extDir, "ai-research-skills")
	extSkillDir := filepath.Join(repoDir, "01-tools", "test-skill")
	if err := os.MkdirAll(extSkillDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(extSkillDir, "SKILL.md"), []byte(`---
name: test-skill
description: External version
tags:
  - design
---
`), 0644); err != nil {
		t.Fatal(err)
	}

	writeManifest(t, extDir, map[string]ExternalRepoState{
		"ai-research-skills": {
			URL:         "https://github.com/example/repo.git",
			CommitSHA:   "abc123",
			License:     "MIT",
			Attribution: "Test",
		},
	})

	builder := NewBuilderWithExternal(nil, nil, dir, extDir)
	cat := builder.BuildSkills("orchestrator", dir)

	if cat.TotalSkills != 1 {
		t.Fatalf("expected 1 skill (dedup), got %d", cat.TotalSkills)
	}
	if cat.Skills[0].Description != "Local version" {
		t.Errorf("expected local skill to win dedup, got description %q", cat.Skills[0].Description)
	}
	if cat.Skills[0].Source != "local" {
		t.Errorf("expected Source='local', got %q", cat.Skills[0].Source)
	}
}

// TestBuildSkills_ExternalDir_MissingManifestSkipsExternal verifies that
// when externalDir is set but MANIFEST.json is missing, external skills
// are skipped and no error occurs.
func TestBuildSkills_ExternalDir_MissingManifestSkipsExternal(t *testing.T) {
	dir := t.TempDir()
	extDir := t.TempDir()
	// No MANIFEST.json in extDir.

	// Create a local skill so catalog isn't empty.
	localSkillDir := filepath.Join(dir, ".opencode", "skills", "local-skill")
	if err := os.MkdirAll(localSkillDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(localSkillDir, "SKILL.md"), []byte(`---
name: local-skill
description: A local skill
---
`), 0644); err != nil {
		t.Fatal(err)
	}

	builder := NewBuilderWithExternal(nil, nil, dir, extDir)
	cat := builder.BuildSkills("orchestrator", dir)

	if cat.TotalSkills != 1 {
		t.Fatalf("expected 1 skill (local only), got %d", cat.TotalSkills)
	}
	if cat.Skills[0].Name != "local-skill" {
		t.Errorf("expected local-skill, got %q", cat.Skills[0].Name)
	}
}

// TestBuildSkills_ExternalDir_AIResearchSkillsLayout verifies the
// AI-Research-SKILLs layout with numbered category dirs.
func TestBuildSkills_ExternalDir_AIResearchSkillsLayout(t *testing.T) {
	dir := t.TempDir()
	extDir := t.TempDir()

	repoDir := filepath.Join(extDir, "ai-research-skills")

	// Create 01-cat/foo/SKILL.md
	fooDir := filepath.Join(repoDir, "01-cat", "foo")
	if err := os.MkdirAll(fooDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(fooDir, "SKILL.md"), []byte(`---
name: foo
description: Foo skill
tags:
  - research
---
`), 0644); err != nil {
		t.Fatal(err)
	}

	// Create 01-cat/bar/SKILL.md
	barDir := filepath.Join(repoDir, "01-cat", "bar")
	if err := os.MkdirAll(barDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(barDir, "SKILL.md"), []byte(`---
name: bar
description: Bar skill
tags:
  - research
---
`), 0644); err != nil {
		t.Fatal(err)
	}

	// Create docs/skip/SKILL.md — should be skipped (no number prefix).
	skipDir := filepath.Join(repoDir, "docs", "skip")
	if err := os.MkdirAll(skipDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(skipDir, "SKILL.md"), []byte(`---
name: skip-me
description: Should be skipped
tags:
  - docs
---
`), 0644); err != nil {
		t.Fatal(err)
	}

	writeManifest(t, extDir, map[string]ExternalRepoState{
		"ai-research-skills": {
			URL:         "https://github.com/example/repo.git",
			CommitSHA:   "abc123",
			License:     "MIT",
			Attribution: "Test",
		},
	})

	builder := NewBuilderWithExternal(nil, nil, dir, extDir)
	cat := builder.BuildSkills("orchestrator", dir)

	names := make(map[string]bool)
	for _, s := range cat.Skills {
		names[s.Name] = true
	}
	if !names["foo"] {
		t.Error("expected 'foo' skill in catalog")
	}
	if !names["bar"] {
		t.Error("expected 'bar' skill in catalog")
	}
	if names["skip-me"] {
		t.Error("expected 'skip-me' to NOT be in catalog (docs dir should be skipped)")
	}
	if cat.TotalSkills != 2 {
		t.Errorf("expected 2 external skills, got %d", cat.TotalSkills)
	}
}

// TestBuildSkills_ExternalDir_UIUXProMaxSkillLayout verifies the
// ui-ux-pro-max-skill layout with .claude/skills/*/SKILL.md.
func TestBuildSkills_ExternalDir_UIUXProMaxSkillLayout(t *testing.T) {
	dir := t.TempDir()
	extDir := t.TempDir()

	repoDir := filepath.Join(extDir, "ui-ux-pro-max-skill")

	// Create .claude/skills/foo/SKILL.md
	fooDir := filepath.Join(repoDir, ".claude", "skills", "foo")
	if err := os.MkdirAll(fooDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(fooDir, "SKILL.md"), []byte(`---
name: foo-ux
description: UX skill foo
tags:
  - design
---
`), 0644); err != nil {
		t.Fatal(err)
	}

	// Create .claude/skills/bar/SKILL.md
	barDir := filepath.Join(repoDir, ".claude", "skills", "bar")
	if err := os.MkdirAll(barDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(barDir, "SKILL.md"), []byte(`---
name: bar-ux
description: UX skill bar
tags:
  - design
---
`), 0644); err != nil {
		t.Fatal(err)
	}

	writeManifest(t, extDir, map[string]ExternalRepoState{
		"ui-ux-pro-max-skill": {
			URL:         "https://github.com/nextlevelbuilder/ui-ux-pro-max-skill.git",
			CommitSHA:   "bdf1179a8b7c6d5e",
			License:     "MIT",
			Attribution: "Copyright 2024 Next Level Builder. Used under MIT License.",
		},
	})

	builder := NewBuilderWithExternal(nil, nil, dir, extDir)
	cat := builder.BuildSkills("orchestrator", dir)

	names := make(map[string]bool)
	for _, s := range cat.Skills {
		names[s.Name] = true
	}
	if !names["foo-ux"] {
		t.Error("expected 'foo-ux' skill in catalog")
	}
	if !names["bar-ux"] {
		t.Error("expected 'bar-ux' skill in catalog")
	}
	if cat.TotalSkills != 2 {
		t.Errorf("expected 2 external skills, got %d", cat.TotalSkills)
	}
}

// TestBuildSkills_ExternalDir_NonexistentDirSkipped verifies that a
// nonexistent externalDir results in no error and no external skills.
func TestBuildSkills_ExternalDir_NonexistentDirSkipped(t *testing.T) {
	dir := t.TempDir()
	extDir := filepath.Join(t.TempDir(), "nonexistent")

	// Create local skill so catalog isn't empty.
	localSkillDir := filepath.Join(dir, ".opencode", "skills", "local-skill")
	if err := os.MkdirAll(localSkillDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(localSkillDir, "SKILL.md"), []byte(`---
name: local-skill
description: A local skill
---
`), 0644); err != nil {
		t.Fatal(err)
	}

	builder := NewBuilderWithExternal(nil, nil, dir, extDir)
	cat := builder.BuildSkills("orchestrator", dir)

	if cat.TotalSkills != 1 {
		t.Fatalf("expected 1 skill (local only), got %d", cat.TotalSkills)
	}
	if cat.Skills[0].Name != "local-skill" {
		t.Errorf("expected local-skill, got %q", cat.Skills[0].Name)
	}
}

// TestBuildSkills_ExternalDir_EmptyManifest verifies that a MANIFEST.json
// with an empty repos map results in no external skills and no error.
func TestBuildSkills_ExternalDir_EmptyManifest(t *testing.T) {
	dir := t.TempDir()
	extDir := t.TempDir()

	// Write manifest with empty repos.
	writeManifest(t, extDir, map[string]ExternalRepoState{})

	// Create local skill.
	localSkillDir := filepath.Join(dir, ".opencode", "skills", "local-skill")
	if err := os.MkdirAll(localSkillDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(localSkillDir, "SKILL.md"), []byte(`---
name: local-skill
description: A local skill
---
`), 0644); err != nil {
		t.Fatal(err)
	}

	builder := NewBuilderWithExternal(nil, nil, dir, extDir)
	cat := builder.BuildSkills("orchestrator", dir)

	if cat.TotalSkills != 1 {
		t.Fatalf("expected 1 skill (local only), got %d", cat.TotalSkills)
	}
	if cat.Skills[0].Name != "local-skill" {
		t.Errorf("expected local-skill, got %q", cat.Skills[0].Name)
	}
}
