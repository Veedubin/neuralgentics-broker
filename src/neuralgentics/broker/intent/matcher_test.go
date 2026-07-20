package intent

import (
	"testing"

	"github.com/Veedubin/neuralgentics-broker/src/neuralgentics/broker/types"
)

func sampleTools() []types.ToolSummary {
	return []types.ToolSummary{
		{Server: "fs", Name: "read_file", Description: "Read a file from disk"},
		{Server: "fs", Name: "write_file", Description: "Write content to a file on disk"},
		{Server: "web", Name: "fetch_url", Description: "Fetch content from a URL"},
		{Server: "db", Name: "query_database", Description: "Execute a SQL query against the database"},
		{Server: "db", Name: "insert_record", Description: "Insert a new record into the database"},
	}
}

func TestTokenize(t *testing.T) {
	tests := []struct {
		input    string
		expected []string
	}{
		{"read file", []string{"read", "file"}},
		{"Read A File From Disk!", []string{"read", "a", "file", "from", "disk"}},
		{"query-database", []string{"query", "database"}},
		{"  multiple   spaces  ", []string{"multiple", "spaces"}},
		{"Fetch content from a URL", []string{"fetch", "content", "from", "a", "url"}},
		{"", nil},
		{"123", []string{"123"}},
	}

	for _, tc := range tests {
		result := tokenize(tc.input)
		if len(result) != len(tc.expected) {
			t.Errorf("tokenize(%q) = %v, want %v", tc.input, result, tc.expected)
			continue
		}
		for i, w := range result {
			if w != tc.expected[i] {
				t.Errorf("tokenize(%q)[%d] = %q, want %q", tc.input, i, w, tc.expected[i])
			}
		}
	}
}

func TestMatch_ExactKeyword(t *testing.T) {
	m := NewMatcher(sampleTools())
	match, err := m.Match("read file")
	if err != nil {
		t.Fatalf("Match failed: %v", err)
	}
	if match.Tool.Name != "read_file" {
		t.Errorf("expected read_file, got %s", match.Tool.Name)
	}
	if match.Score < DefaultThreshold {
		t.Errorf("score %.2f below threshold %.2f", match.Score, DefaultThreshold)
	}
}

func TestMatch_PartialKeyword(t *testing.T) {
	m := NewMatcher(sampleTools())
	match, err := m.Match("file read")
	if err != nil {
		t.Fatalf("Match failed: %v", err)
	}
	// Should match read_file since both "file" and "read" appear.
	if match.Tool.Name != "read_file" {
		t.Errorf("expected read_file, got %s", match.Tool.Name)
	}
}

func TestMatch_DatabaseQuery(t *testing.T) {
	m := NewMatcher(sampleTools())
	match, err := m.Match("query the database")
	if err != nil {
		t.Fatalf("Match failed: %v", err)
	}
	if match.Tool.Name != "query_database" {
		t.Errorf("expected query_database, got %s", match.Tool.Name)
	}
}

func TestMatch_NoMatch(t *testing.T) {
	m := NewMatcher(sampleTools())
	_, err := m.Match("fly rocket orbit")
	if err == nil {
		t.Fatal("expected error for no match")
	}
}

func TestMatch_EmptyIntent(t *testing.T) {
	m := NewMatcher(sampleTools())
	_, err := m.Match("")
	if err == nil {
		t.Fatal("expected error for empty intent")
	}
}

func TestMatch_EmptyTools(t *testing.T) {
	m := NewMatcher([]types.ToolSummary{})
	_, err := m.Match("read file")
	if err == nil {
		t.Fatal("expected error with no tools")
	}
}

func TestMatchAll(t *testing.T) {
	m := NewMatcher(sampleTools())
	matches := m.MatchAll("file", 3)
	if len(matches) == 0 {
		t.Fatal("expected at least one match")
	}
	// Both read_file and write_file contain "file" in description/name.
	if len(matches) > 3 {
		t.Errorf("expected at most 3 matches, got %d", len(matches))
	}
	// Verify descending score order.
	for i := 1; i < len(matches); i++ {
		if matches[i].Score > matches[i-1].Score {
			t.Errorf("matches not sorted by score: %v > %v", matches[i].Score, matches[i-1].Score)
		}
	}
}

func TestMatchAll_Limit(t *testing.T) {
	m := NewMatcher(sampleTools())
	matches := m.MatchAll("file", 1)
	if len(matches) != 1 {
		t.Errorf("expected 1 match, got %d", len(matches))
	}
}

func TestMatchAll_ZeroLimit(t *testing.T) {
	m := NewMatcher(sampleTools())
	matches := m.MatchAll("file", 0)
	// Limit 0 means return all matches.
	if len(matches) == 0 {
		t.Fatal("expected all matches when limit=0")
	}
}

func TestScoreTool_NameBonus(t *testing.T) {
	// Test that matching words in the tool name get a bonus.
	tools := []types.ToolSummary{
		{Server: "fs", Name: "file_operations", Description: "General operations"},
		{Server: "fs", Name: "other_tool", Description: "Read a file from disk"},
	}
	m := NewMatcher(tools)
	match, err := m.Match("file")
	if err != nil {
		t.Fatalf("Match failed: %v", err)
	}
	// Should prefer file_operations because "file" is in the name.
	if match.Tool.Name != "file_operations" {
		t.Errorf("expected file_operations due to name bonus, got %s", match.Tool.Name)
	}
}

func TestMatch_SubstringMatch(t *testing.T) {
	m := NewMatcher(sampleTools())
	// "fetch" should match "fetch_url" via substring.
	match, err := m.Match("fetch data")
	if err != nil {
		t.Fatalf("Match failed: %v", err)
	}
	if match.Tool.Name != "fetch_url" {
		t.Errorf("expected fetch_url, got %s", match.Tool.Name)
	}
}

// --- Jaccard similarity tests ---

func jaccardTools() []types.ToolSummary {
	return []types.ToolSummary{
		{Server: "fs", Name: "remove_file", Description: "Delete a file from disk"},
		{Server: "mem", Name: "search_memories", Description: "Search through stored memories"},
		{Server: "mem", Name: "create_entity", Description: "Create a new entity in the knowledge graph"},
		{Server: "fs", Name: "write_file", Description: "Write data to a file on disk"},
		{Server: "fs", Name: "read_file", Description: "Read a file from disk"},
	}
}

func TestJaccard_SynonymMatch(t *testing.T) {
	// "delete file" should match "remove_file" — Jaccard catches related tokens
	m := NewMatcher(jaccardTools())
	match, err := m.Match("delete file")
	if err != nil {
		t.Fatalf("Match failed: %v", err)
	}
	if match.Tool.Name != "remove_file" {
		t.Errorf("expected remove_file, got %s", match.Tool.Name)
	}
	if match.Score < DefaultThreshold {
		t.Errorf("score %.3f below threshold %.2f", match.Score, DefaultThreshold)
	}
}

func TestJaccard_SearchMemories(t *testing.T) {
	// "search my memories" should match "search_memories"
	m := NewMatcher(jaccardTools())
	match, err := m.Match("search my memories")
	if err != nil {
		t.Fatalf("Match failed: %v", err)
	}
	if match.Tool.Name != "search_memories" {
		t.Errorf("expected search_memories, got %s", match.Tool.Name)
	}
}

func TestJaccard_CreateEntity(t *testing.T) {
	// "create new entity" should match "create_entity"
	m := NewMatcher(jaccardTools())
	match, err := m.Match("create new entity")
	if err != nil {
		t.Fatalf("Match failed: %v", err)
	}
	if match.Tool.Name != "create_entity" {
		t.Errorf("expected create_entity, got %s", match.Tool.Name)
	}
}

func TestJaccard_WriteDataToDisk(t *testing.T) {
	// "write data to disk" should match "write_file"
	m := NewMatcher(jaccardTools())
	match, err := m.Match("write data to disk")
	if err != nil {
		t.Fatalf("Match failed: %v", err)
	}
	if match.Tool.Name != "write_file" {
		t.Errorf("expected write_file, got %s", match.Tool.Name)
	}
}

func TestJaccard_NoMatchReturnsError(t *testing.T) {
	m := NewMatcher(jaccardTools())
	_, err := m.Match("launch spacecraft orbit")
	if err == nil {
		t.Fatal("expected error for no matching tool")
	}
}

func TestJaccardSimilarity_Unit(t *testing.T) {
	tests := []struct {
		a, b     []string
		expected float64
	}{
		{[]string{"read", "file"}, []string{"read", "file"}, 1.0},
		{[]string{"read", "file"}, []string{"write", "file"}, 1.0 / 3.0},
		{[]string{"read"}, []string{}, 0},
		{[]string{}, []string{}, 0},
		{[]string{"a", "b", "c"}, []string{"b", "c", "d"}, 2.0 / 4.0},
	}
	for _, tc := range tests {
		result := jaccardSimilarity(tc.a, tc.b)
		if tc.expected == 0 && result != 0 {
			t.Errorf("jaccardSimilarity(%v, %v) = %f, want 0", tc.a, tc.b, result)
		} else if tc.expected != 0 && (result < tc.expected-0.01 || result > tc.expected+0.01) {
			t.Errorf("jaccardSimilarity(%v, %v) = %f, want ~%f", tc.a, tc.b, result, tc.expected)
		}
	}
}

func TestRemoveStopWords(t *testing.T) {
	tests := []struct {
		input    []string
		expected []string
	}{
		{[]string{"search", "my", "memories"}, []string{"search", "memories"}},
		{[]string{"the", "a", "an"}, []string{"the", "a", "an"}}, // all stop words => fallback keeps all
		{[]string{"read", "file"}, []string{"read", "file"}},     // no stop words
		{[]string{}, []string(nil)},
	}
	for _, tc := range tests {
		result := removeStopWords(tc.input)
		if len(result) != len(tc.expected) {
			t.Errorf("removeStopWords(%v) = %v, want %v", tc.input, result, tc.expected)
			continue
		}
		for i, w := range result {
			if w != tc.expected[i] {
				t.Errorf("removeStopWords(%v)[%d] = %q, want %q", tc.input, i, w, tc.expected[i])
			}
		}
	}
}

func TestSimpleStem(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"deleting", "delet"},   // -ing removed
		{"writing", "write"},    // -ing, double consonant + e
		{"searching", "search"}, // -ing removed
		{"creation", "crea"},    // -tion removed
		{"goodness", "good"},    // -ness removed
		{"walked", "walk"},      // -ed removed
		{"quickly", "quick"},    // -ly removed
		{"read", "read"},        // no suffix
	}
	for _, tc := range tests {
		result := simpleStem(tc.input)
		if result != tc.expected {
			t.Errorf("simpleStem(%q) = %q, want %q", tc.input, result, tc.expected)
		}
	}
}
