package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

const nhlTeamAbbrev = "MTL"

type nhlTeamRef struct {
	Abbrev     string `json:"abbrev"`
	CommonName struct {
		Default string `json:"default"`
	} `json:"commonName"`
	Score *int `json:"score"`
}

type nhlGame struct {
	ID           int64      `json:"id"`
	GameDate     string     `json:"gameDate"` // club-local date, YYYY-MM-DD
	StartTimeUTC string     `json:"startTimeUTC"`
	GameState    string     `json:"gameState"` // FUT, PRE, LIVE, CRIT, OFF, FINAL
	AwayTeam     nhlTeamRef `json:"awayTeam"`
	HomeTeam     nhlTeamRef `json:"homeTeam"`
	Venue        struct {
		Default string `json:"default"`
	} `json:"venue"`
}

type nhlScheduleResp struct {
	ClubTimezone string    `json:"clubTimezone"`
	Games        []nhlGame `json:"games"`
}

func fetchJSON(url string, out interface{}) error {
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "habs-news-agent/1.0")
	resp, err := httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP %d fetching %s", resp.StatusCode, url)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	return json.Unmarshal(body, out)
}

// FetchWeekSchedule returns MTL's games for the current week (NHL club-schedule endpoint).
func FetchWeekSchedule() (*nhlScheduleResp, error) {
	var sched nhlScheduleResp
	url := fmt.Sprintf("https://api-web.nhle.com/v1/club-schedule/%s/week/now", nhlTeamAbbrev)
	if err := fetchJSON(url, &sched); err != nil {
		return nil, fmt.Errorf("fetching NHL schedule: %w", err)
	}
	return &sched, nil
}

// GameOnDate returns the game (if any) whose club-local gameDate matches the given date (YYYY-MM-DD).
func GameOnDate(sched *nhlScheduleResp, dateStr string) *nhlGame {
	for i := range sched.Games {
		if sched.Games[i].GameDate == dateStr {
			return &sched.Games[i]
		}
	}
	return nil
}

// IsFinal reports whether a game's state indicates it has completed.
func IsFinal(g *nhlGame) bool {
	if g == nil {
		return false
	}
	return g.GameState == "OFF" || g.GameState == "FINAL"
}

func RecapText(g *nhlGame) string {
	if g == nil {
		return ""
	}
	away := g.AwayTeam.CommonName.Default
	home := g.HomeTeam.CommonName.Default
	as, hs := "?", "?"
	if g.AwayTeam.Score != nil {
		as = fmt.Sprintf("%d", *g.AwayTeam.Score)
	}
	if g.HomeTeam.Score != nil {
		hs = fmt.Sprintf("%d", *g.HomeTeam.Score)
	}
	return fmt.Sprintf("%s %s @ %s %s (final) at %s", away, as, home, hs, g.Venue.Default)
}

// ---------- Standings (season context, best-effort) ----------

type nhlStandingsResp struct {
	Standings []struct {
		TeamAbbrev struct {
			Default string `json:"default"`
		} `json:"teamAbbrev"`
		Wins               int    `json:"wins"`
		Losses             int    `json:"losses"`
		OtLosses           int    `json:"otLosses"`
		Points             int    `json:"points"`
		GamesPlayed        int    `json:"gamesPlayed"`
		ConferenceName     string `json:"conferenceName"`
		ConferenceSequence int    `json:"conferenceSequence"`
		StreakCode         string `json:"streakCode"`
		StreakCount        int    `json:"streakCount"`
	} `json:"standings"`
}

// FetchMTLStanding returns a one-line standings summary for Montreal, or "" if unavailable.
//
// Caught live while testing this: api-web.nhle.com/v1/standings/now does NOT
// return zero games played during the off-season/preseason -- it keeps
// serving the most recently COMPLETED season's final table (e.g. 82 GP)
// right up until the new season's games start counting. A naive
// "GamesPlayed == 0 means no data" check therefore misses that case
// entirely and would silently present a stale record as if it were live.
// We can't reliably tell "stale prior-season" apart from "live current-season"
// from this endpoint alone without cross-referencing the schedule, so instead
// we're explicit in the label itself rather than pretending certainty either way.
func FetchMTLStanding() string {
	var s nhlStandingsResp
	if err := fetchJSON("https://api-web.nhle.com/v1/standings/now", &s); err != nil {
		return ""
	}
	for _, row := range s.Standings {
		if row.TeamAbbrev.Default == nhlTeamAbbrev {
			if row.GamesPlayed == 0 {
				return ""
			}
			return fmt.Sprintf("%d-%d-%d, %d pts, %d GP, conf rank %d, streak %s%d (most recent record on file -- may be last completed season's final if the new season hasn't started yet; cross-check the date)",
				row.Wins, row.Losses, row.OtLosses, row.Points, row.GamesPlayed,
				row.ConferenceSequence, row.StreakCode, row.StreakCount)
		}
	}
	return ""
}

func todayHalifax() (string, time.Time) {
	loc, err := time.LoadLocation("America/Halifax")
	if err != nil {
		loc = time.FixedZone("AST", -3*60*60)
	}
	now := time.Now().In(loc)
	return now.Format("2006-01-02"), now
}
