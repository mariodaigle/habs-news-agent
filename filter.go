package main

import (
	"regexp"
	"sort"
	"strings"
)

// keyword sets checked in priority order -- first match wins. Every keyword
// is matched at word boundaries (see compiledCategoryKeywords), NOT as a raw
// substring: an earlier version used strings.Contains with "ir " as a stand-in
// for the injured-reserve abbreviation "IR", which silently matched inside
// ordinary words like "their" (t-h-e-IR- ) and miscategorized at least one
// real headline ("What Is the Canadiens' Ceiling in 2026-27?" got flagged as
// an injury story because of the "ir " in "their"). Caught by running real
// fetched data through this function -- see README testing notes.
var categoryKeywords = []struct {
	category string
	words    []string
}{
	{CatRecap, []string{
		"recap", "final score", "box score", "highlights", "final", "win over",
		"defeat", "shutout", "overtime winner", "shootout", "hat trick", "postgame",
		"post-game", "game review", "3 stars", "three stars",
	}},
	{CatTrades, []string{
		"trade", "traded", "rumor", "rumour", "waiver", "waivers", "claimed off",
		"signs", "signing", "re-sign", "resign", "extension", "offer sheet",
		"free agent", "free agency", "buyout", "cap space", "acquire", "acquired",
		"deadline", "contract",
	}},
	{CatInjuries, []string{
		"injury", "injured", "injuries", "ir", "il", "day-to-day", "day to day",
		"lineup", "line combo", "line combination", "goalie start", "starting goalie",
		"recall", "reassign", "reassigned", "scratch", "scratched", "out indefinitely",
		"lower-body", "upper-body", "lower body", "upper body",
		"surgery", "surgical", "surgeries", "rehab", "concussion",
	}},
	{CatProspect, []string{
		"prospect", "rocket", "laval", "ahl", "draft", "development camp",
		"junior", "qmjhl", "chl", "world juniors", "top 25 under 25", "rookie camp",
	}},
}

// wordBoundaryRE turns a keyword/phrase into a regex that only matches at
// word boundaries, so short/ambiguous tokens ("ir", "il", "ahl") can't fire
// on a substring buried inside an unrelated word.
func wordBoundaryRE(phrase string) *regexp.Regexp {
	escaped := regexp.QuoteMeta(phrase)
	escaped = strings.ReplaceAll(escaped, ` `, `\s+`)
	return regexp.MustCompile(`\b` + escaped + `\b`)
}

type compiledCategory struct {
	category string
	patterns []*regexp.Regexp
}

var compiledCategoryKeywords = func() []compiledCategory {
	out := make([]compiledCategory, 0, len(categoryKeywords))
	for _, ck := range categoryKeywords {
		cc := compiledCategory{category: ck.category}
		for _, w := range ck.words {
			cc.patterns = append(cc.patterns, wordBoundaryRE(w))
		}
		out = append(out, cc)
	}
	return out
}()

var whitespaceRE = regexp.MustCompile(`\s+`)
var punctRE = regexp.MustCompile(`[^a-z0-9 ]`)

func normalizeTitle(s string) string {
	s = strings.ToLower(s)
	s = punctRE.ReplaceAllString(s, " ")
	s = whitespaceRE.ReplaceAllString(s, " ")
	return strings.TrimSpace(s)
}

// Categorize assigns a Category to each item based on keyword matching (at
// word boundaries, see above) in the title + summary. This is a blunt
// instrument by design -- it's meant to sort news into rough buckets for the
// digest prompt, not to be a precise classifier. The LLM summarization step
// does the real judgment call about what's actually important.
func Categorize(items []Item) {
	for i := range items {
		hay := strings.ToLower(items[i].Title + " " + items[i].Summary)
		items[i].Category = CatGeneral
		for _, cc := range compiledCategoryKeywords {
			matched := false
			for _, re := range cc.patterns {
				if re.MatchString(hay) {
					matched = true
					break
				}
			}
			if matched {
				items[i].Category = cc.category
				break
			}
		}
	}
}

// Dedup removes items with a duplicate normalized title or duplicate link,
// keeping the first (most information-dense source ordering is the
// caller's responsibility -- pass higher-priority sources first).
func Dedup(items []Item) []Item {
	seenTitle := map[string]bool{}
	seenLink := map[string]bool{}
	out := make([]Item, 0, len(items))
	for _, it := range items {
		nt := normalizeTitle(it.Title)
		if nt == "" {
			continue
		}
		if seenTitle[nt] || (it.Link != "" && seenLink[it.Link]) {
			continue
		}
		seenTitle[nt] = true
		if it.Link != "" {
			seenLink[it.Link] = true
		}
		out = append(out, it)
	}
	return out
}

// SortByRecency sorts newest first. Items with no parseable date sink to the bottom.
func SortByRecency(items []Item) {
	sort.SliceStable(items, func(i, j int) bool {
		return items[i].Published.After(items[j].Published)
	})
}

// GroupByCategory buckets items and caps each bucket at maxPerCat (most recent first).
func GroupByCategory(items []Item, maxPerCat int) map[string][]Item {
	groups := map[string][]Item{}
	for _, it := range items {
		groups[it.Category] = append(groups[it.Category], it)
	}
	for cat, list := range groups {
		SortByRecency(list)
		if len(list) > maxPerCat {
			list = list[:maxPerCat]
		}
		groups[cat] = list
	}
	return groups
}
