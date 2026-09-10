package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

// Verified current at api docs.claude.com (models/overview) on 2026-09-09:
// "claude-sonnet-5" is the current Sonnet API model ID, $2/$10 per MTok
// in/out. The model this code originally shipped with (claude-sonnet-4-5,
// dated Sept 2025) is now the legacy/previous-generation Sonnet -- still
// answers, but there's no reason to default to a superseded model. If this
// has aged out again by the time you're reading it, check
// platform.claude.com/docs/en/models/overview and update this constant (or
// just set the CLAUDE_MODEL secret, which overrides it without a code change).
const defaultClaudeModel = "claude-sonnet-5"

type anthropicRequest struct {
	Model     string             `json:"model"`
	MaxTokens int                `json:"max_tokens"`
	System    string             `json:"system"`
	Messages  []anthropicMessage `json:"messages"`
}

type anthropicMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type anthropicResponse struct {
	Content []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"content"`
	Error *struct {
		Type    string `json:"type"`
		Message string `json:"message"`
	} `json:"error"`
}

// buildRawDigestPrompt renders every collected item, grouped by category, as
// plain text for the LLM (or for the no-LLM fallback) to work from.
func buildRawDigestPrompt(groups map[string][]Item, gameFact string, standing string, slot string, dateStr string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Run slot: %s\nDate: %s\n", slot, dateStr)
	if standing != "" {
		fmt.Fprintf(&b, "Current MTL standing: %s\n", standing)
	}
	if gameFact != "" {
		fmt.Fprintf(&b, "VERIFIED final score from NHL API (use this exactly, do not alter the numbers): %s\n", gameFact)
	}
	b.WriteString("\n")
	for _, cat := range categoryOrder {
		items := groups[cat]
		if len(items) == 0 {
			continue
		}
		fmt.Fprintf(&b, "== %s ==\n", cat)
		for _, it := range items {
			pub := ""
			if !it.Published.IsZero() {
				pub = it.Published.Format("Jan 2 15:04 MST")
			}
			fmt.Fprintf(&b, "- [%s] %s (%s) %s\n  %s\n", it.Source, it.Title, pub, it.Link, it.Summary)
		}
		b.WriteString("\n")
	}
	return b.String()
}

const systemPrompt = `You write a terse, no-fluff Montreal Canadiens news digest for a die-hard fan who explicitly does not want validation or hype -- he wants precise, hedged, honest analysis.

Rules:
- Organize output under these headers, in this order, and SKIP any header with nothing under it: Game Recap & Performance, Trades & Roster Moves, Injuries & Lineup, Prospects & Laval Rocket (AHL), Other Habs News.
- Under each header, write tight bullet points. Lead with the fact, not the framing.
- Explicitly flag speculation and rumours as such (e.g. "unconfirmed", "per [source], not corroborated elsewhere") -- never present a rumour as settled fact.
- If a verified final score is provided, use those exact numbers -- never invent or round stats.
- Do not editorialize with hype language ("huge", "massive", "exciting"). If something is genuinely notable, say why in plain terms.
- Cite the source outlet inline for anything non-obvious, e.g. "(Yardbarker)" or "(Habs Eyes on the Prize)".
- If two sources conflict, say so instead of picking one silently.
- End with a one-line "Nothing else notable" only if a section is thin, not if it's empty (empty sections are just omitted).
- Keep the whole digest under ~500 words. This runs multiple times a day -- don't repeat context the fan already has, focus on what's new since a typical prior digest.
- Output plain markdown, no preamble like "Here is your digest".`

// Summarize calls the Anthropic API to turn raw items into a written digest.
// If ANTHROPIC_API_KEY is not set, or the call fails, it falls back to a
// plain (non-LLM) formatted listing so the pipeline never produces nothing.
func Summarize(groups map[string][]Item, gameFact, standing, slot, dateStr string) (string, error) {
	raw := buildRawDigestPrompt(groups, gameFact, standing, slot, dateStr)

	apiKey := os.Getenv("ANTHROPIC_API_KEY")
	if apiKey == "" {
		return fallbackFormat(groups, gameFact, standing), fmt.Errorf("ANTHROPIC_API_KEY not set, used plain-text fallback formatting instead of an LLM summary")
	}

	model := os.Getenv("CLAUDE_MODEL")
	if model == "" {
		model = defaultClaudeModel
	}

	reqBody := anthropicRequest{
		Model:     model,
		MaxTokens: 1500,
		System:    systemPrompt,
		Messages: []anthropicMessage{
			{Role: "user", Content: raw},
		},
	}
	payload, err := json.Marshal(reqBody)
	if err != nil {
		return fallbackFormat(groups, gameFact, standing), err
	}

	req, err := http.NewRequest("POST", "https://api.anthropic.com/v1/messages", bytes.NewReader(payload))
	if err != nil {
		return fallbackFormat(groups, gameFact, standing), err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", apiKey)
	req.Header.Set("anthropic-version", "2023-06-01")

	client := &http.Client{Timeout: 60 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return fallbackFormat(groups, gameFact, standing), fmt.Errorf("calling Anthropic API: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fallbackFormat(groups, gameFact, standing), err
	}

	var ar anthropicResponse
	if err := json.Unmarshal(body, &ar); err != nil {
		return fallbackFormat(groups, gameFact, standing), fmt.Errorf("parsing Anthropic response: %w (raw: %s)", err, truncate(string(body), 300))
	}
	if ar.Error != nil {
		return fallbackFormat(groups, gameFact, standing), fmt.Errorf("Anthropic API error (%s): %s -- check CLAUDE_MODEL is a current model id at docs.anthropic.com/en/docs/about-claude/models", ar.Error.Type, ar.Error.Message)
	}
	if len(ar.Content) == 0 {
		return fallbackFormat(groups, gameFact, standing), fmt.Errorf("Anthropic API returned no content")
	}
	return strings.TrimSpace(ar.Content[0].Text), nil
}

// fallbackFormat produces a readable digest with no LLM involved, so the
// agent still emits something useful if the API key is missing or the call fails.
func fallbackFormat(groups map[string][]Item, gameFact, standing string) string {
	var b strings.Builder
	if standing != "" {
		fmt.Fprintf(&b, "_Standing: %s_\n\n", standing)
	}
	if gameFact != "" {
		fmt.Fprintf(&b, "**%s**\n\n", gameFact)
	}
	any := false
	for _, cat := range categoryOrder {
		items := groups[cat]
		if len(items) == 0 {
			continue
		}
		any = true
		fmt.Fprintf(&b, "### %s\n", cat)
		for _, it := range items {
			fmt.Fprintf(&b, "- [%s](%s) — %s\n", it.Title, it.Link, it.Source)
		}
		b.WriteString("\n")
	}
	if !any {
		b.WriteString("No categorized items collected this run.\n")
	}
	return b.String()
}
