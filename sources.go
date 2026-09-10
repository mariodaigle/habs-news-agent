package main

import (
	"encoding/json"
	"encoding/xml"
	"fmt"
	"html"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"
)

var httpClient = &http.Client{Timeout: 20 * time.Second}

// ---------- RSS ----------

type rssFeed struct {
	Channel struct {
		Items []rssItem `xml:"item"`
	} `xml:"channel"`
}

type rssItem struct {
	Title       string `xml:"title"`
	Link        string `xml:"link"`
	PubDate     string `xml:"pubDate"`
	Description string `xml:"description"`
	// content:encoded, namespace-qualified so it actually matches.
	ContentEncoded string `xml:"http://purl.org/rss/1.0/modules/content/ encoded"`
}

var pubDateLayouts = []string{
	time.RFC1123Z,
	time.RFC1123,
	"Mon, 2 Jan 2006 15:04:05 -0700",
	"Mon, 2 Jan 2006 15:04:05 MST",
	"2006-01-02T15:04:05Z07:00",
}

func parsePubDate(s string) time.Time {
	s = strings.TrimSpace(s)
	for _, layout := range pubDateLayouts {
		if t, err := time.Parse(layout, s); err == nil {
			return t
		}
	}
	return time.Time{}
}

var htmlTagRE = regexp.MustCompile(`<[^>]*>`)

func stripHTML(s string) string {
	s = htmlTagRE.ReplaceAllString(s, " ")
	s = html.UnescapeString(s)
	s = strings.Join(strings.Fields(s), " ")
	return s
}

func truncate(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "\xe2\x80\xa6" // ellipsis
}

// FetchRSS fetches and parses a standard RSS 2.0 feed into normalized Items.
func FetchRSS(url, sourceName string) ([]Item, error) {
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "habs-news-agent/1.0 (+personal digest bot)")
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetching %s: %w", sourceName, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("fetching %s: HTTP %d", sourceName, resp.StatusCode)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", sourceName, err)
	}

	var feed rssFeed
	if err := xml.Unmarshal(body, &feed); err != nil {
		return nil, fmt.Errorf("parsing %s: %w", sourceName, err)
	}

	items := make([]Item, 0, len(feed.Channel.Items))
	for _, it := range feed.Channel.Items {
		snippet := it.ContentEncoded
		if snippet == "" {
			snippet = it.Description
		}
		items = append(items, Item{
			Title:     strings.TrimSpace(it.Title),
			Link:      strings.TrimSpace(it.Link),
			Source:    sourceName,
			Published: parsePubDate(it.PubDate),
			Summary:   truncate(stripHTML(snippet), 500),
		})
	}
	return items, nil
}

// ---------- Reddit (best-effort, optional) ----------

type redditListing struct {
	Data struct {
		Children []struct {
			Data struct {
				Title         string  `json:"title"`
				URL           string  `json:"url"`
				Permalink     string  `json:"permalink"`
				Score         int     `json:"score"`
				NumComments   int     `json:"num_comments"`
				CreatedUTC    float64 `json:"created_utc"`
				LinkFlairText string  `json:"link_flair_text"`
				Selftext      string  `json:"selftext"`
				IsSelf        bool    `json:"is_self"`
			} `json:"data"`
		} `json:"children"`
	} `json:"data"`
}

// FetchRedditHot pulls hot posts from a subreddit's public JSON listing.
// This endpoint is unauthenticated and Reddit has been known to rate-limit
// or block it without warning -- callers MUST treat errors here as
// non-fatal. See README "Known limitations".
func FetchRedditHot(subreddit string, limit int) ([]Item, error) {
	url := fmt.Sprintf("https://www.reddit.com/r/%s/hot.json?limit=%d", subreddit, limit)
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}
	ua := "habs-news-agent/1.0 (personal digest bot; contact: github repo owner)"
	req.Header.Set("User-Agent", ua)
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetching r/%s: %w", subreddit, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("fetching r/%s: HTTP %d (reddit's public JSON endpoint is unauthenticated and frequently blocks bots -- this is expected to fail sometimes)", subreddit, resp.StatusCode)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	var listing redditListing
	if err := json.Unmarshal(body, &listing); err != nil {
		return nil, fmt.Errorf("parsing r/%s response: %w", subreddit, err)
	}

	items := make([]Item, 0, len(listing.Data.Children))
	for _, c := range listing.Data.Children {
		d := c.Data
		// Skip low-signal self-posts (game threads, memes) unless flaired as news/rumour.
		flair := strings.ToLower(d.LinkFlairText)
		if d.IsSelf && !strings.Contains(flair, "news") && !strings.Contains(flair, "rumour") && !strings.Contains(flair, "rumor") && !strings.Contains(flair, "discussion") {
			continue
		}
		link := "https://www.reddit.com" + d.Permalink
		snippet := d.Selftext
		if snippet == "" {
			snippet = fmt.Sprintf("(%d upvotes, %d comments)", d.Score, d.NumComments)
		}
		items = append(items, Item{
			Title:     strings.TrimSpace(d.Title),
			Link:      link,
			Source:    "r/Habs",
			Published: time.Unix(int64(d.CreatedUTC), 0).UTC(),
			Summary:   truncate(stripHTML(snippet), 300),
		})
	}
	return items, nil
}
