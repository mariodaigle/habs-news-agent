package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// WriteDigestFile writes the digest to digests/YYYY-MM-DD-<slot>.md and
// refreshes digests/latest.md. Returns the path written.
func WriteDigestFile(dir, dateStr, slot, digest string, sourceErrs []string) (string, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	fname := fmt.Sprintf("%s-%s.md", dateStr, slot)
	path := filepath.Join(dir, fname)

	var b bytes.Buffer
	fmt.Fprintf(&b, "# Habs Digest — %s (%s)\n\n", dateStr, slot)
	fmt.Fprintf(&b, "_Generated %s_\n\n", time.Now().UTC().Format(time.RFC3339))
	b.WriteString(digest)
	b.WriteString("\n")
	if len(sourceErrs) > 0 {
		b.WriteString("\n---\n_Source warnings this run (non-fatal):_\n")
		for _, e := range sourceErrs {
			fmt.Fprintf(&b, "- %s\n", e)
		}
	}

	if err := os.WriteFile(path, b.Bytes(), 0o644); err != nil {
		return "", err
	}
	latest := filepath.Join(dir, "latest.md")
	if err := os.WriteFile(latest, b.Bytes(), 0o644); err != nil {
		return path, err
	}
	return path, nil
}

// RegenerateIndex rewrites the repo-root index.md that GitHub Pages serves
// as the site's homepage, listing every digest in dir (newest first, since
// filenames are YYYY-MM-DD-<slot>.md and sort lexically = chronologically).
//
// This relies entirely on GitHub Pages' built-in Jekyll processing -- every
// .md file in the repo gets auto-rendered to HTML at the same path (e.g.
// digests/2026-09-09-morning.md -> .../digests/2026-09-09-morning.html) with
// zero extra build tooling, as long as Pages is set to "Deploy from a
// branch" / main / (root). This function just has to produce a root
// index.md (Jekyll prefers index.md over README.md as the homepage) that
// links to what's actually there.
func RegenerateIndex(rootDir, digestDir string) error {
	entries, err := os.ReadDir(digestDir)
	if err != nil {
		return err
	}
	var names []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		if strings.HasSuffix(e.Name(), ".md") && e.Name() != "latest.md" {
			names = append(names, e.Name())
		}
	}
	sort.Sort(sort.Reverse(sort.StringSlice(names)))

	var b bytes.Buffer
	b.WriteString("---\ntitle: Habs News Agent\n---\n\n")
	b.WriteString("# Habs News Agent\n\n")
	b.WriteString("Automated Montreal Canadiens news digest -- trades/rumours, injuries/lineup, game recaps, and prospects/Laval Rocket (AHL), refreshed multiple times a day. See the repo's [README](README.html) for how this works.\n\n")
	fmt.Fprintf(&b, "**[Latest digest](%s)**\n\n", htmlHref(filepath.Join(filepath.Base(digestDir), "latest.md")))
	b.WriteString("## Archive\n\n")
	if len(names) == 0 {
		b.WriteString("No digests written yet -- the schedule hasn't fired, or this is a fresh repo. Trigger it manually from the Actions tab.\n")
	}
	for _, n := range names {
		href := htmlHref(filepath.Join(filepath.Base(digestDir), n))
		label := strings.TrimSuffix(n, ".md")
		fmt.Fprintf(&b, "- [%s](%s)\n", label, href)
	}

	return os.WriteFile(filepath.Join(rootDir, "index.md"), b.Bytes(), 0o644)
}

// htmlHref converts a source markdown path to the URL Jekyll will actually
// serve it at (same path, .md swapped for .html).
func htmlHref(mdPath string) string {
	return strings.TrimSuffix(mdPath, ".md") + ".html"
}

// PostToDiscord sends the digest to a Discord webhook, chunked to respect
// Discord's 2000-char message content limit. No-op if webhookURL is empty.
func PostToDiscord(webhookURL, title, digest string) error {
	if webhookURL == "" {
		return nil
	}
	const maxLen = 1900
	full := title + "\n\n" + digest
	chunks := chunkString(full, maxLen)
	for _, c := range chunks {
		payload, err := json.Marshal(map[string]string{"content": c})
		if err != nil {
			return err
		}
		resp, err := http.Post(webhookURL, "application/json", bytes.NewReader(payload))
		if err != nil {
			return fmt.Errorf("posting to Discord: %w", err)
		}
		resp.Body.Close()
		if resp.StatusCode >= 300 {
			return fmt.Errorf("Discord webhook returned HTTP %d", resp.StatusCode)
		}
		time.Sleep(500 * time.Millisecond) // avoid Discord rate limits between chunks
	}
	return nil
}

func chunkString(s string, maxLen int) []string {
	r := []rune(s)
	if len(r) <= maxLen {
		return []string{s}
	}
	var chunks []string
	for len(r) > 0 {
		n := maxLen
		if n > len(r) {
			n = len(r)
		}
		chunks = append(chunks, string(r[:n]))
		r = r[n:]
	}
	return chunks
}
