package main

import (
	"encoding/json"
	"encoding/xml"
	"strings"
	"testing"
)

// Sample RSS mirroring the real habseyesontheprize.com / Yardbarker structure
// (verified live before writing this parser), including the namespaced
// content:encoded tag, to catch any XML-decoding mistakes.
const sampleRSS = `<?xml version="1.0" encoding="UTF-8"?>
<rss version="2.0" xmlns:content="http://purl.org/rss/1.0/modules/content/" xmlns:dc="http://purl.org/dc/elements/1.1/">
<channel>
  <title>Habs Eyes on the Prize</title>
  <item>
    <title>Monday Habs Headlines: Ivan Demidov went against agent's advice</title>
    <link>https://www.habseyesontheprize.com/example-article</link>
    <dc:creator>Some Author</dc:creator>
    <pubDate>Mon, 07 Sep 2026 08:00:00 +0000</pubDate>
    <category>Habs Headlines</category>
    <guid isPermaLink="false">123</guid>
    <description>&lt;p&gt;Short teaser about Demidov signing an eight-year deal.&lt;/p&gt;</description>
    <content:encoded><![CDATA[<p>Full body with a <a href="#">trade</a> rumor mention and more detail than the description.</p>]]></content:encoded>
  </item>
</channel>
</rss>`

func TestRSSParsing(t *testing.T) {
	var feed rssFeed
	if err := xml.Unmarshal([]byte(sampleRSS), &feed); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}
	if len(feed.Channel.Items) != 1 {
		t.Fatalf("expected 1 item, got %d", len(feed.Channel.Items))
	}
	it := feed.Channel.Items[0]
	if !strings.Contains(it.Title, "Ivan Demidov") {
		t.Errorf("title not parsed correctly: %q", it.Title)
	}
	if it.Link != "https://www.habseyesontheprize.com/example-article" {
		t.Errorf("link not parsed correctly: %q", it.Link)
	}
	if !strings.Contains(it.ContentEncoded, "trade") {
		t.Errorf("content:encoded (namespaced tag) not parsed correctly, got: %q", it.ContentEncoded)
	}
	pub := parsePubDate(it.PubDate)
	if pub.IsZero() {
		t.Errorf("pubDate failed to parse: %q", it.PubDate)
	}

	stripped := stripHTML(it.ContentEncoded)
	if strings.Contains(stripped, "<") {
		t.Errorf("stripHTML left tags behind: %q", stripped)
	}
}

// Sample JSON mirroring the real api-web.nhle.com club-schedule response
// (verified live before writing this parser).
const sampleSchedule = `{
  "clubTimezone": "America/Montreal",
  "clubUTCOffset": "-04:00",
  "games": [
    {
      "id": 2026020123,
      "gameDate": "2026-11-05",
      "startTimeUTC": "2026-11-06T00:00:00Z",
      "gameState": "OFF",
      "awayTeam": {"abbrev": "MTL", "commonName": {"default": "Canadiens"}, "score": 4},
      "homeTeam": {"abbrev": "BOS", "commonName": {"default": "Bruins"}, "score": 2},
      "venue": {"default": "TD Garden"}
    },
    {
      "id": 2026020124,
      "gameDate": "2026-11-08",
      "startTimeUTC": "2026-11-08T23:00:00Z",
      "gameState": "FUT",
      "awayTeam": {"abbrev": "TOR", "commonName": {"default": "Maple Leafs"}, "score": null},
      "homeTeam": {"abbrev": "MTL", "commonName": {"default": "Canadiens"}, "score": null},
      "venue": {"default": "Bell Centre"}
    }
  ]
}`

func TestScheduleParsingAndGameLookup(t *testing.T) {
	var sched nhlScheduleResp
	if err := json.Unmarshal([]byte(sampleSchedule), &sched); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}
	if len(sched.Games) != 2 {
		t.Fatalf("expected 2 games, got %d", len(sched.Games))
	}

	g := GameOnDate(&sched, "2026-11-05")
	if g == nil {
		t.Fatal("expected to find game on 2026-11-05")
	}
	if !IsFinal(g) {
		t.Errorf("expected game with state OFF to be final")
	}
	recap := RecapText(g)
	if !strings.Contains(recap, "Canadiens 4") || !strings.Contains(recap, "Bruins 2") {
		t.Errorf("recap text missing expected score: %q", recap)
	}

	future := GameOnDate(&sched, "2026-11-08")
	if future == nil {
		t.Fatal("expected to find future game")
	}
	if IsFinal(future) {
		t.Errorf("FUT game should not be reported as final")
	}

	missing := GameOnDate(&sched, "2026-12-25")
	if missing != nil {
		t.Errorf("expected no game found for a date with no entry")
	}
}

func TestCategorizeAndDedup(t *testing.T) {
	items := []Item{
		{Title: "Canadiens trade rumor swirls around Slafkovsky", Summary: ""},
		{Title: "Kaiden Guhle day-to-day with lower-body injury", Summary: ""},
		{Title: "Laval Rocket prospect scores in AHL debut", Summary: ""},
		{Title: "Habs defeat Bruins 4-2, recap and highlights", Summary: ""},
		{Title: "Some unrelated Habs merchandise story", Summary: ""},
		// duplicate of the first item, different casing/punctuation -- should be deduped
		{Title: "Canadiens Trade Rumor Swirls Around Slafkovsky!", Summary: ""},
	}
	Categorize(items)
	want := []string{CatTrades, CatInjuries, CatProspect, CatRecap, CatGeneral, CatTrades}
	for i, w := range want {
		if items[i].Category != w {
			t.Errorf("item %d (%q): got category %q, want %q", i, items[i].Title, items[i].Category, w)
		}
	}

	deduped := Dedup(items)
	if len(deduped) != 5 {
		t.Errorf("expected dedup to drop 1 duplicate (5 remaining), got %d", len(deduped))
	}
}
