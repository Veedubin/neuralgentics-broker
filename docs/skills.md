# Skills

The broker ships a **SkillCatalog** that aggregates skill definitions from
two sources and presents them to the intent matcher as a role-filtered
view. The implementation lives in
`src/neuralgentics/broker/catalog/skills.go` and
`src/neuralgentics/broker/catalog/skill_cache.go`.

## Local and external skills

A skill is a short markdown (or YAML-front-mattered markdown) file that
describes a capability an agent can invoke. The broker knows about two
sources:

| Source | Where | Description |
|--------|-------|-------------|
| `local` | Workspace `.opencode/skills/` directory | Skills shipped with the project |
| `external` | `--external-skills-dir` (e.g. `~/.neuralgentics/external-skills`) | Skills fetched from third-party repos |

The catalog merges both sources into a single `SkillCatalog` struct keyed
by role. Each `SkillSummary` carries `Source` (`"local"` or
`"external"`), tags (merged from the YAML body and the front-matter),
`AgentScope` (roles this skill is visible to), and a `Path` relative to
the workspace root.

## Provenance manifests

Every external skill is stamped with an `ExternalProvenance` record so the
broker can attribute it and track trust over time:

```go
type ExternalProvenance struct {
    Repo        string // e.g. "ai-research-skills"
    CommitSHA   string // full git SHA at fetch time
    License     string // e.g. "MIT"
    Attribution string // human-readable copyright + license line
}
```

The full manifest of every external repo is written to `MANIFEST.json` in
the external skills directory by the external-skills-fetcher. The broker
loads `MANIFEST.json` at catalog build time; a missing manifest is treated
as "no external skills" (graceful degradation — the fetcher will recreate
it on the next session).

The manifest schema is versioned (`ExternalManifest.Version`). Each repo
entry (`ExternalRepoState`) records the URL, commit SHA, license,
attribution, refresh timestamp, and status (`cloned`, `updated`,
`skipped-network-error`, or `skipped-disabled`).

## LRU body cache

Reading a skill file from disk on every catalog query is expensive when
the workspace is large. The broker keeps an LRU cache of recently-read
skill bodies in `src/neuralgentics/broker/catalog/skill_cache.go`. The
cache:

- is keyed by absolute path
- evicts the least-recently-used entry when it fills
- is invalidated on `SIGHUP` so config + skills reload together
- returns a cache miss as a sentinel, so callers can fall back to a disk
  read without paying for a second lookup

## Scope file

Agent-to-skill visibility is defined in an `agent-skill-scope.yaml` file:

```yaml
version: 1
roles:
  coder:
    - code-generation
    - refactoring
  architect:
    - design
    - research
```

The catalog merges the scope file's tag lists with the per-skill
`AgentScope` front-matter so a skill can be visible to a role either
because the skill says so or because the scope file grants it.