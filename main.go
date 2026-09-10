package main

import (
	"flag"
	"fmt"
	"log"
	"os"
)

// news sources checked on every run, in priority order (priority matters for Dedup).
var rssSources = []struct {
	url  string
	name string
}{
	{"https://www.habseyesontheprize.com/rss/index.xml", "Habs Eyes on the Prize"},
	{"https://www.yardbarker.com/rss/team/nhl/montreal_canadiens", "Yardbarker"},
}

func main() {
	slot := flag.String("slot", os.Getenv("RUN_SLOT"), "run slot: morning, evening, or postgame")
	digestDir := flag.String("dir", "digests", "directory to write digest files into")
	flag.Parse()

	if *slot == "" {
		*slot = "manual"
	}

	dateStr, _ := todayHalifax()

	var sourceErrs []string
	var allItems []Item

	for _, src := range rssSources {
		items, err := FetchRSS(src.url, src.name)
		if err != nil {
			log.Printf("warning: %v", err)
			sourceErrs = append(sourceErrs, err.Error())
			continue
		}
		allItems = append(allItems, items...)
	}

	redditItems, err := FetchRedditHot("Habs", 25)
	if err != nil {
		log.Printf("warning (non-fatal, reddit is best-effort): %v", err)
		sourceErrs = append(sourceErrs, err.Error())
	} else {
		allItems = append(allItems, redditItems...)
	}

	sched, err := FetchWeekSchedule()
	var gameFact string
	gameCompletedToday := false
	if err != nil {
		log.Printf("warning: %v", err)
		sourceErrs = append(sourceErrs, err.Error())
	} else {
		if g := GameOnDate(sched, dateStr); g != nil && IsFinal(g) {
			gameCompletedToday = true
			gameFact = RecapText(g)
		}
	}

	// The postgame slot only exists to deliver a recap. If nothing finished
	// today, there's nothing for it to say -- exit cleanly, write nothing,
	// so the workflow skips the commit step instead of creating noise.
	if *slot == "postgame" && !gameCompletedToday {
		log.Println("postgame slot: no completed MTL game today, nothing to report, exiting without writing a digest")
		return
	}

	standing := FetchMTLStanding()

	allItems = Dedup(allItems)
	Categorize(allItems)
	groups := GroupByCategory(allItems, 8)

	digest, err := Summarize(groups, gameFact, standing, *slot, dateStr)
	if err != nil {
		log.Printf("warning: %v", err)
		sourceErrs = append(sourceErrs, err.Error())
	}

	path, err := WriteDigestFile(*digestDir, dateStr, *slot, digest, sourceErrs)
	if err != nil {
		log.Fatalf("failed to write digest file: %v", err)
	}
	fmt.Printf("wrote %s\n", path)

	if err := RegenerateIndex(".", *digestDir); err != nil {
		// Non-fatal: the digest itself is already written and committed either
		// way. Worst case the Pages homepage is briefly stale.
		log.Printf("warning: failed to regenerate index.md: %v", err)
	}

	webhook := os.Getenv("DISCORD_WEBHOOK_URL")
	if webhook != "" {
		title := fmt.Sprintf("**Habs Digest — %s (%s)**", dateStr, *slot)
		if err := PostToDiscord(webhook, title, digest); err != nil {
			log.Printf("warning: discord post failed: %v", err)
		}
	}
}
