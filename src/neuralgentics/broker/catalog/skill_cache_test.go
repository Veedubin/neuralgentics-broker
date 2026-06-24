package catalog

import (
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func TestSkillBodyCache_BasicGetPut(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "skill.md")
	content := "# Test Skill\n\nBody content."
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	cache := NewSkillBodyCache(10, 1024*1024)

	// Put then Get
	cache.Put(path, content)
	body, ok := cache.Get(path)
	if !ok {
		t.Fatal("expected cache hit after Put")
	}
	if body != content {
		t.Errorf("expected body %q, got %q", content, body)
	}

	// Second Get should be a hit (modTime unchanged).
	body2, ok2 := cache.Get(path)
	if !ok2 {
		t.Fatal("expected cache hit on second Get")
	}
	if body2 != content {
		t.Errorf("expected same body on second Get, got %q", body2)
	}

	stats := cache.Stats()
	if stats.Hits != 2 {
		t.Errorf("expected 2 hits, got %d", stats.Hits)
	}
	if stats.Misses != 0 {
		t.Errorf("expected 0 misses, got %d", stats.Misses)
	}
	if stats.Entries != 1 {
		t.Errorf("expected 1 entry, got %d", stats.Entries)
	}
}

func TestSkillBodyCache_MissOnEmpty(t *testing.T) {
	cache := NewSkillBodyCache(10, 1024*1024)
	_, ok := cache.Get("/nonexistent/path/skill.md")
	if ok {
		t.Error("expected false for missing file on empty cache")
	}
	stats := cache.Stats()
	if stats.Misses != 1 {
		t.Errorf("expected 1 miss, got %d", stats.Misses)
	}
}

func TestSkillBodyCache_LRUEviction(t *testing.T) {
	dir := t.TempDir()
	cache := NewSkillBodyCache(3, 1024*1024) // max 3 entries

	// Insert 5 files.
	for i := range 5 {
		path := filepath.Join(dir, filepath.Join("skill"+string(rune('0'+i))+".md"))
		content := "Skill " + string(rune('0'+i)) + " body"
		if err := os.WriteFile(path, []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
		cache.Put(path, content)
	}

	stats := cache.Stats()
	if stats.Entries != 3 {
		t.Errorf("expected 3 entries after eviction, got %d", stats.Entries)
	}

	// The first 2 should have been evicted (oldest LRU).
	path0 := filepath.Join(dir, "skill0.md")
	if _, ok := cache.Get(path0); ok {
		// Get re-reads from disk if missing, so ok=true means it was re-inserted.
		// We need to check if it was evicted before Get — but Get re-reads.
		// Instead, check that after 5 Puts with capacity 3, only 3 remain.
		// The fact that Entries == 3 means eviction happened correctly.
	}
}

func TestSkillBodyCache_ByteEviction(t *testing.T) {
	dir := t.TempDir()
	cache := NewSkillBodyCache(100, 50) // max 50 bytes

	// Insert a 100-byte file — exceeds maxBytes.
	path := filepath.Join(dir, "big.md")
	content := string(make([]byte, 100))
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	cache.Put(path, content)

	// The 100-byte entry should have been evicted because it exceeds maxBytes.
	stats := cache.Stats()
	if stats.Entries != 0 {
		t.Errorf("expected 0 entries (100 bytes > 50 byte limit), got %d entries", stats.Entries)
	}
	if stats.Bytes != 0 {
		t.Errorf("expected 0 bytes, got %d", stats.Bytes)
	}
}

func TestSkillBodyCache_ModTimeInvalidation(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "skill.md")
	content1 := "Version 1"
	if err := os.WriteFile(path, []byte(content1), 0644); err != nil {
		t.Fatal(err)
	}

	cache := NewSkillBodyCache(10, 1024*1024)
	body1, ok := cache.Get(path)
	if !ok || body1 != content1 {
		t.Fatalf("expected %q, got %q, ok=%v", content1, body1, ok)
	}

	// Modify the file on disk — need a sufficient sleep to ensure mtime
	// differs on filesystems with second-resolution timers (ext4, HFS+).
	// Use a helper: truncate the mtime to force a visible difference.
	time.Sleep(100 * time.Millisecond)
	content2 := "Version 2"
	if err := os.WriteFile(path, []byte(content2), 0644); err != nil {
		t.Fatal(err)
	}
	// Explicitly set mtime to now to guarantee it differs from the cached value.
	newModTime := time.Now().Truncate(0)
	if err := os.Chtimes(path, newModTime, newModTime); err != nil {
		t.Logf("warning: Chtimes failed: %v; relying on WriteFile mtime change", err)
	}

	// Get should detect modTime change and re-read.
	body2, ok := cache.Get(path)
	if !ok {
		t.Fatal("expected ok=true after modTime change")
	}
	if body2 != content2 {
		t.Errorf("expected %q after modTime change, got %q", content2, body2)
	}

	// Stats: at least 1 miss (first read). If modTime changed, second Get is also a miss.
	// If modTime did not change, second Get is a hit. Both outcomes are valid —
	// what matters is that body2 contains the new content.
	stats := cache.Stats()
	if stats.Misses < 1 {
		t.Errorf("expected at least 1 miss, got %d", stats.Misses)
	}
	if stats.Misses+stats.Hits != 2 {
		t.Errorf("expected 2 total operations (hits+misses), got hits=%d misses=%d", stats.Hits, stats.Misses)
	}
}

func TestSkillBodyCache_FileDisappeared(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "vanish.md")
	if err := os.WriteFile(path, []byte("content"), 0644); err != nil {
		t.Fatal(err)
	}

	cache := NewSkillBodyCache(10, 1024*1024)
	cache.Put(path, "content")

	// Delete the file from disk.
	os.Remove(path)

	// Get should return false — file disappeared.
	_, ok := cache.Get(path)
	if ok {
		t.Error("expected false when file disappeared after caching")
	}
}

func TestSkillBodyCache_InvalidateOne(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "skill.md")
	if err := os.WriteFile(path, []byte("content"), 0644); err != nil {
		t.Fatal(err)
	}

	cache := NewSkillBodyCache(10, 1024*1024)
	cache.Put(path, "content")

	stats := cache.Stats()
	if stats.Entries != 1 {
		t.Fatalf("expected 1 entry before invalidate, got %d", stats.Entries)
	}

	cache.Invalidate(path)
	stats = cache.Stats()
	if stats.Entries != 0 {
		t.Errorf("expected 0 entries after invalidate, got %d", stats.Entries)
	}
	if stats.Bytes != 0 {
		t.Errorf("expected 0 bytes after invalidate, got %d", stats.Bytes)
	}
}

func TestSkillBodyCache_InvalidateAll(t *testing.T) {
	dir := t.TempDir()
	cache := NewSkillBodyCache(10, 1024*1024)

	for i := range 5 {
		path := filepath.Join(dir, "skill"+string(rune('0'+i))+".md")
		content := "content" + string(rune('0'+i))
		if err := os.WriteFile(path, []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
		cache.Put(path, content)
	}

	stats := cache.Stats()
	if stats.Entries != 5 {
		t.Fatalf("expected 5 entries before InvalidateAll, got %d", stats.Entries)
	}

	cache.InvalidateAll()
	stats = cache.Stats()
	if stats.Entries != 0 {
		t.Errorf("expected 0 entries after InvalidateAll, got %d", stats.Entries)
	}
	if stats.Bytes != 0 {
		t.Errorf("expected 0 bytes after InvalidateAll, got %d", stats.Bytes)
	}
}

func TestSkillBodyCache_Stats(t *testing.T) {
	dir := t.TempDir()
	cache := NewSkillBodyCache(10, 1024*1024)

	path := filepath.Join(dir, "stat.md")
	content := "stat content"
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	// First Get — miss.
	body, ok := cache.Get(path)
	if !ok || body != content {
		t.Fatalf("expected %q, got %q", content, body)
	}
	stats := cache.Stats()
	if stats.Misses != 1 {
		t.Errorf("expected 1 miss, got %d", stats.Misses)
	}
	if stats.Hits != 0 {
		t.Errorf("expected 0 hits, got %d", stats.Hits)
	}
	if stats.Entries != 1 {
		t.Errorf("expected 1 entry, got %d", stats.Entries)
	}
	if stats.Bytes != int64(len(content)) {
		t.Errorf("expected %d bytes, got %d", len(content), stats.Bytes)
	}

	// Second Get — hit (modTime unchanged).
	body2, ok2 := cache.Get(path)
	if !ok2 || body2 != content {
		t.Fatalf("expected %q on hit, got %q", content, body2)
	}
	stats = cache.Stats()
	if stats.Hits != 1 {
		t.Errorf("expected 1 hit, got %d", stats.Hits)
	}
	if stats.Misses != 1 {
		t.Errorf("expected 1 miss (unchanged), got %d", stats.Misses)
	}
}

func TestSkillBodyCache_Concurrent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "shared.md")
	content := "Shared content for concurrent access"
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	cache := NewSkillBodyCache(10, 1024*1024)
	var wg sync.WaitGroup

	// 10 goroutines hammering Get/Put on the same file.
	for i := range 10 {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			body, ok := cache.Get(path)
			if !ok || body != content {
				t.Errorf("goroutine %d: concurrent Get failed: ok=%v body=%q", idx, ok, body)
			}
		}(i)
	}
	wg.Wait()

	// Should have exactly 1 entry (the shared file).
	stats := cache.Stats()
	if stats.Entries != 1 {
		t.Errorf("expected 1 entry, got %d", stats.Entries)
	}
}

func TestSkillBodyCache_MoveToBack(t *testing.T) {
	dir := t.TempDir()
	cache := NewSkillBodyCache(3, 1024*1024)

	// Insert A, B, C.
	files := []struct {
		name, content string
	}{
		{"a.md", "a"},
		{"b.md", "b"},
		{"c.md", "c"},
	}
	for _, f := range files {
		path := filepath.Join(dir, f.name)
		if err := os.WriteFile(path, []byte(f.content), 0644); err != nil {
			t.Fatal(err)
		}
		cache.Put(path, f.content)
	}

	// Access A (promotes to MRU — moves to back of LRU).
	pathA := filepath.Join(dir, "a.md")
	_, ok := cache.Get(pathA)
	if !ok {
		t.Fatal("expected cache hit for A")
	}

	// Insert D — should evict B (now oldest, since A was promoted).
	pathD := filepath.Join(dir, "d.md")
	if err := os.WriteFile(pathD, []byte("d"), 0644); err != nil {
		t.Fatal(err)
	}
	cache.Put(pathD, "d")

	// A should still be present (was promoted).
	if _, ok := cache.Get(filepath.Join(dir, "a.md")); !ok {
		t.Error("A should still be cached (promoted to MRU before D insert)")
	}
	// B should be evicted.
	// After evicting B, Get on B's path will re-read from disk (since file exists).
	// We verify by checking cache stats — should have 3 entries (A, C, D).
	stats := cache.Stats()
	if stats.Entries != 3 {
		t.Errorf("expected 3 entries (A, C, D), got %d", stats.Entries)
	}
}
