# habs-news-agent

A standalone Go program, run on a schedule by GitHub Actions, that pulls Montreal Canadiens news/rumours from a few sources, filters and categorizes it, writes a synthesized digest to this repo, and (optionally) pushes it to Discord.

This is deliberately **not** a chat-wrapper — there's no Claude session involved at runtime. It's a real program with its own fetch/filter/categorize logic; the only place an LLM enters is one optional API call at the end to turn the collected raw items into readable prose (and it degrades gracefully to a plain bullet list if that call is skipped or fails).

## What it does, each run

1. Fetches:
   - [Habs Eyes on the Prize](https://www.habseyesontheprize.com/rss/index.xml) RSS (SB Nation's Canadiens site — daily headline roundups + original analysis)
   - [Yardbarker](https://www.yardbarker.com/rss/team/nhl/montreal_canadiens) RSS (Canadiens-specific feed)
   - r/Habs "hot" listing (best-effort — see Known limitations)
   - NHL's official API (`api-web.nhle.com`) for the current week's schedule/scores and season standings
2. Dedupes items (by normalized title and by link) and sorts by category:
   - Game Recap & Performance
   - Trades & Roster Moves
   - Injuries & Lineup
   - Prospects & Laval Rocket (AHL)
   - Other Habs News
3. If `ANTHROPIC_API_KEY` is set, sends the categorized raw items to the Claude API with a system prompt that explicitly instructs it to flag rumours as unconfirmed, cite sources, use verified NHL API scores exactly (never invented stats), and stay terse. If the key isn't set, or the call fails for any reason, it falls back to a plain formatted list — the pipeline never silently produces nothing.
4. Writes `digests/YYYY-MM-DD-<slot>.md` and refreshes `digests/latest.md`.
5. If `DISCORD_WEBHOOK_URL` is set, posts the digest there too (chunked to respect Discord's 2000-character limit).

## Schedule

Three runs a day via `.github/workflows/digest.yml`, tuned for `America/Halifax`:

| Slot | Local time (ADT, UTC-3) | Purpose |
|---|---|---|
| `morning` | ~08:00 | Overnight news/rumours |
| `evening` | ~17:00 | Pregame lineup/injury news |
| `postgame` | ~23:00 | Game recap — **only writes a file if MTL actually played and the game is final**; otherwise it exits cleanly and the workflow skips the commit |

**Important honest caveat:** GitHub Actions cron is fixed UTC and does not shift for daylight saving. These three cron lines are set for ADT (UTC-3, in effect roughly mid-March to early November). Twice a year, when Halifax switches between AST and ADT, the actual local run times will be off by an hour until you edit the three `cron:` lines in the workflow file by ±1 hour. I didn't build DST-awareness into this because it adds real complexity (you'd need the workflow to compute the offset itself, e.g. via a scheduled bootstrap job) for a problem that's a 30-second manual fix twice a year — flagging that trade-off rather than hiding it.

## Setup

1. Create a new GitHub repo and push this code to it. **Must be Public** if you want free GitHub Pages hosting for the web view (Step 5) — Pages on private repos requires GitHub Pro or higher. Nothing in this repo is sensitive (secrets live in GitHub's encrypted Actions secrets, never in code), so public is the pragmatic default.
2. In the repo's **Settings → Secrets and variables → Actions**, add:
   - `ANTHROPIC_API_KEY` (required for the LLM-written digest; get one at [console.anthropic.com](https://console.anthropic.com)). Without it, you still get a working digest, just as a plain source listing instead of synthesized prose.
   - `CLAUDE_MODEL` (optional) — defaults to `claude-sonnet-5` in code (confirmed current at [platform.claude.com/docs/en/models/overview](https://platform.claude.com/docs/en/models/overview) as of 2026-09-09, priced at $2/$10 per million input/output tokens). **Re-verify this is still current** before relying on it long-term — model IDs get deprecated eventually, and if this one has aged out by the time you're reading this, the run will fail over to the plain-list fallback and log the exact error, not crash silently.
   - Note: this is billed through your Anthropic **API** account (console.anthropic.com, pay-as-you-go with a payment method on file), which is separate from any Claude.ai subscription — it doesn't draw down a chat plan's usage. At this program's actual token volume (see the architecture discussion in the project doc / ask Claude), expect well under $2/month.
   - `DISCORD_WEBHOOK_URL` (optional) — a Discord channel webhook URL if you want push-style delivery. Create one via a Discord channel's Settings → Integrations → Webhooks. If you'd rather use Slack, swap `PostToDiscord` in `output.go` for a Slack incoming-webhook POST — same shape, different payload key (`{"text": ...}` instead of `{"content": ...}`).
3. Actions should be enabled by default on push. Confirm under the repo's **Actions** tab.
4. Test it immediately without waiting for the schedule: **Actions → Habs Digest → Run workflow**, pick a slot (`morning`/`evening`/`postgame`), run it, then check `digests/latest.md` and the Action's logs.
5. **To publish a web view (free):** repo **Settings → Pages → Build and deployment → Source: "Deploy from a branch" → Branch: `main`, folder: `/ (root)`** → Save. That's the entire setup — no extra build step needed. GitHub Pages runs Jekyll automatically on every push to `main`, and every `.md` file in this repo (including everything under `digests/`) gets rendered to HTML at the same path (`digests/2026-09-09-morning.md` → `.../digests/2026-09-09-morning.html`). `index.md` at the repo root is the homepage Jekyll serves by default, and the program regenerates it on every run (`RegenerateIndex` in `output.go`) with a link to the latest digest plus a full reverse-chronological archive — so the workflow's existing `git push` step is also what publishes the site; there's no separate deploy step to babysit. `_config.yml` sets a clean built-in theme (`jekyll-theme-minimal`) and a title — swap the theme name for any other [GitHub Pages-supported theme](https://pages.github.com/themes/) if you want a different look. Give it a minute or two after the first push (or the first scheduled/manual run) for Pages to build; the URL is `https://<your-username>.github.io/<repo-name>/`, shown in the Pages settings page once live.

## Known limitations (read before trusting this blindly)

- **Reddit access is unverified and likely to be flaky.** Reddit's public unauthenticated JSON endpoints (`reddit.com/r/Habs/hot.json`) have been increasingly rate-limited/blocked since their 2023 API changes. This code hits them with a descriptive User-Agent and treats any failure as non-fatal (logged as a warning, run continues) — but don't be surprised if it fails often or eventually stops working entirely. If it does, the fix is a real Reddit API app (OAuth client credentials) rather than the public JSON endpoint; that's a bigger change than fit the scope of this build, flagging it rather than pretending it's solved.
- **Two editorial sources, not ten.** I verified `habseyesontheprize.com` and `yardbarker.com`'s RSS feeds are live and Canadiens-specific before writing the parser against their real structure (also tested NHL.com's team RSS, Montreal Gazette, Sportsnet, TSN, Daily Faceoff, and ESPN's team-news endpoint — all either 404, robots-blocked, or not actually team-filtered). Two solid, verified, Habs-dedicated sources beats five fragile guessed ones. Add more in `main.go`'s `rssSources` slice if you find others worth trusting — `FetchRSS` is generic and will parse any standard RSS 2.0 feed.
- **No structured injury data exists.** The NHL's official API does not expose injury/IR status (confirmed by inspecting the live roster endpoint) — all injury/lineup information in the digest comes from what the editorial sources report, not a database, so it's only as current as the last time someone wrote about it.
- **The keyword categorizer is blunt on purpose.** It's a cheap first-pass bucketing (see `filter.go`) to organize what gets handed to the LLM step — it is not a precise classifier and will sometimes misfile something (e.g. a headline mentioning both a trade and an injury goes wherever the first keyword match lands). The actual judgment call about what matters happens in the summarization step.
- **This was built and unit-tested in a sandboxed environment with no live network access to these domains** (fixtures in `parse_test.go` mirror real, verified response shapes captured before writing the parsers, and `go test ./...` passes against them) — but I could not run the compiled binary against live traffic end-to-end from here. Run it manually via `workflow_dispatch` right after setup and actually read the output before trusting the schedule.

## Repo layout

```
main.go          orchestration: fetch -> dedup -> categorize -> summarize -> write -> notify
sources.go       RSS + Reddit fetchers, HTML stripping
nhl.go           NHL official API: schedule/scores, standings, game-day detection
filter.go        keyword categorization, dedup, grouping
summarize.go     Claude API call + system prompt + no-LLM fallback formatter
output.go        markdown file writer, Discord webhook poster
parse_test.go    unit tests against fixture data mirroring verified real responses
digests/         written output (committed by the Action each run)
```

No third-party Go dependencies — everything is stdlib (`net/http`, `encoding/xml`, `encoding/json`). That's a deliberate choice: for something this size, pulling in an RSS library is more supply-chain surface than it's worth.
