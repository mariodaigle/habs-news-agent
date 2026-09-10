package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRegenerateIndex(t *testing.T) {
	root := t.TempDir()
	digestDir := filepath.Join(root, "digests")
	if err := os.MkdirAll(digestDir, 0o755); err != nil {
		t.Fatal(err)
	}
	files := []string{"2026-09-05-morning.md", "2026-09-09-evening.md", "2026-09-09-morning.md", "latest.md"}
	for _, f := range files {
		if err := os.WriteFile(filepath.Join(digestDir, f), []byte("# test\ncontent\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	if err := RegenerateIndex(root, digestDir); err != nil {
		t.Fatalf("RegenerateIndex failed: %v", err)
	}

	out, err := os.ReadFile(filepath.Join(root, "index.md"))
	if err != nil {
		t.Fatalf("index.md not written: %v", err)
	}
	content := string(out)

	if strings.Contains(content, "latest.md") {
		t.Errorf("latest.md should not appear in the archive list itself, got:\n%s", content)
	}
	if !strings.Contains(content, "digests/latest.html") {
		t.Errorf("expected a link to digests/latest.html, got:\n%s", content)
	}
	if !strings.Contains(content, "digests/2026-09-09-evening.html") {
		t.Errorf("expected 2026-09-09-evening entry with .html extension, got:\n%s", content)
	}

	// newest-first ordering
	posEvening := strings.Index(content, "2026-09-09-evening")
	posMorning9 := strings.Index(content, "2026-09-09-morning")
	posMorning5 := strings.Index(content, "2026-09-05-morning")
	if !(posMorning9 < posEvening && posEvening < posMorning5) {
		t.Errorf("expected reverse-chronological order (09-09-morning, then 09-09-evening, then 09-05-morning), got positions %d %d %d\n%s", posMorning9, posEvening, posMorning5, content)
	}
}
