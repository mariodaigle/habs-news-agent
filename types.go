package main

import "time"

// Item is a normalized news/rumour item pulled from any source.
type Item struct {
	Title     string
	Link      string
	Source    string
	Published time.Time
	Summary   string // short plain-text snippet, HTML stripped
	Category  string // assigned by categorize()
}

const (
	CatTrades   = "Trades & Roster Moves"
	CatInjuries = "Injuries & Lineup"
	CatRecap    = "Game Recap & Performance"
	CatProspect = "Prospects & Laval Rocket (AHL)"
	CatGeneral  = "Other Habs News"
)

// categoryOrder controls the order sections appear in the digest.
var categoryOrder = []string{CatRecap, CatTrades, CatInjuries, CatProspect, CatGeneral}
