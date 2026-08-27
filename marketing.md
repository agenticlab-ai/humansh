# Marketing playbook

How to talk about humansh in public: which channels, in what order, when to post, and ready-to-paste copy for each. Written 2026-08-27, after the first [Show HN](https://news.ycombinator.com/item?id=49460715).

- [What we learned from the first Show HN](#what-we-learned-from-the-first-show-hn)
- [Core messaging](#core-messaging)
- [Objections and answers](#objections-and-answers)
- [Prep checklist (before any more posting)](#prep-checklist-before-any-more-posting)
- [Channels](#channels)
  - [1. GitHub itself](#1-github-itself)
  - [2. Reddit](#2-reddit)
  - [3. X/Twitter](#3-xtwitter)
  - [4. Mastodon and Bluesky](#4-mastodon-and-bluesky)
  - [5. Hacker News, round 2](#5-hacker-news-round-2)
  - [6. Dev.to / long-form technical posts](#6-devto--long-form-technical-posts)
  - [7. Product Hunt](#7-product-hunt)
  - [8. Tool directories](#8-tool-directories)
  - [9. Newsletters](#9-newsletters)
  - [10. Awesome lists](#10-awesome-lists)
  - [11. Package managers as discovery](#11-package-managers-as-discovery)
  - [12. Comment marketing (evergreen)](#12-comment-marketing-evergreen)
  - [13. Lower priority: Lobsters, LinkedIn, YouTube, podcasts](#13-lower-priority-lobsters-linkedin-youtube-podcasts)
- [Launch calendar](#launch-calendar)
- [Rules of engagement](#rules-of-engagement)
- [What to measure](#what-to-measure)

---

## What we learned from the first Show HN

[The post](https://news.ycombinator.com/item?id=49460715) (2026-08-27, 3 points, 4 comments) underperformed for fixable reasons:

1. **The title didn't name the product or the differentiator.** "Forgot the Command? Stay in the Terminal" reads as one of dozens of "AI writes your shell command" tools. Nothing in the title says *review-first*, *inside your real shell*, or *uses the subscription you already pay for* — the three things that make humansh not-a-clone.
2. **Posted at 06:38 UTC — 2:38am US Eastern.** HN's voting audience is asleep. Show HN needs Tue–Thu, 8–11am ET (13:00–15:00 UTC during daylight saving).
3. **It linked humansh.com, not the repo.** For open-source tools, HN engages far more with a GitHub link — the README is stronger than any landing page for that audience, and "MIT, Go, here's the source" is itself a hook.
4. **The comments told us exactly which objections the messaging must preempt:**
   - *"this repo could almost be a single file"* (with an ollama one-liner) — the pitch must make clear the LLM call is the trivial 5%. The product is everything around it: the deterministic classifier that keeps `git status` instant and offline, ZLE/Readline integration so the command lands editable in your real prompt, never-auto-execute plus the high-risk gate, and provider isolation.
   - *"Atuin already solves that very well"* — position against history search explicitly: Atuin finds commands you've already run; humansh writes ones you've never run.
   - *"looks great. i still not able to figure out why i am not using it"* — the copy needs a concrete trigger moment, not a capability description. The moment is: **you were about to alt-tab to a chatbot for an `ffmpeg`/`find`/`awk`/`tar` incantation.**

A repost is legitimate: HN's FAQ explicitly allows a small number of reposts for stories that got no significant attention. Do it 3+ weeks later, tied to a release (see [Hacker News, round 2](#5-hacker-news-round-2)).

---

## Core messaging

### One-liners (pick per context)

- **Default:** Type plain English at your shell prompt. Get a real command back — to review, not to run.
- **The trigger moment:** The moment you'd alt-tab to a chatbot for an ffmpeg/find/awk incantation — just type the English where you already are.
- **Against the crowd:** Not a new terminal, not a REPL, not an agent, not a wrapper. Your own zsh/bash, with one new trick.
- **For AI-subscription holders:** Already pay for Claude Code, Codex, or Cursor? humansh reuses that CLI. No new API key, no new bill.
- **Safety-first framing:** It never runs anything. Every generated command lands in your editable prompt; nothing executes until you press Enter yourself.

### The five differentiators, ranked

Lead with 1–3 for terminal audiences, 4 for AI-tool audiences. All claims below are documented in the README — public copy must stay copy-pasteable from README/docs; if it isn't documented, don't claim it.

1. **Never auto-executes.** Commands are inserted for review; high-risk output needs a second, deliberate key sequence.
2. **Runs in your shell, not a wrapper.** Integration at the ZLE/Readline layer — `cd`, `export`, aliases, functions, job control all behave normally.
3. **Stays quiet when you type real commands.** A local, deterministic, inspectable classifier (not an LLM) decides before any network call. `git status` costs no latency and no quota. Genuinely ambiguous input is left untouched.
4. **Uses your existing provider CLI.** Codex, Claude Code, or Cursor own auth and billing. OpenRouter metered billing is opt-in only, with no silent fallback.
5. **Sends very little, verifiably.** Your request, shell, OS, arch, a privacy-normalised directory label, detected tools. Never history, env vars, file contents, or username. Secrets live in the OS keychain. MIT, single Go binary, no sudo, checksum-verified install, clean uninstall.

### Audience → message map

| Audience | Where they are | Lead with |
|---|---|---|
| Terminal power users | HN, r/commandline, r/zsh, r/bash, Lobsters | #1–#3: review-first, real shell, deterministic classifier |
| AI-tool subscribers | r/ClaudeAI, Codex/Cursor communities, X | #4: reuse the subscription you already pay for |
| Go developers | r/golang, Gophers Slack, Golang Weekly | Engineering: PTY tests, race detector, architecture-enforced boundaries |
| FOSS/privacy crowd | Mastodon, r/opensource | #5: MIT, minimal data, keychain, honest uninstall |
| Generalist devs | Product Hunt, newsletters, dev.to | The trigger moment + the demo GIF |

---

## Objections and answers

Have these ready before every post; paste-adapt, never argue.

**"This could be a one-line alias to ollama/sgpt."**
> Totally — for the LLM call, it could. The call is the easy 5%. The rest is the product: a deterministic local classifier so `git status` never touches a provider and ambiguous input is left alone; ZLE/Readline integration so the result lands *editable in your real prompt* instead of stdout; a risk gate so `rm -rf` output needs a deliberate second keystroke; and each provider CLI runs isolated in an empty temp dir with a minimal environment. That's also why the repo isn't one file.

**"Atuin / history search already does this."**
> Different job: Atuin finds commands you've already run — it's great, use both. humansh writes commands you've never run (or can never remember the flags for).

**"How is this different from GitHub Copilot CLI / Warp / zsh-codex?"**
> Copilot CLI is a separate `gh copilot` invocation with its own subscription; Warp is a replacement terminal. humansh lives inside your existing zsh/bash at the line-editor layer, never auto-executes, decides English-vs-command locally before any network call, and reuses whichever of Codex/Claude Code/Cursor you already pay for.

**"curl | bash, seriously?"**
> The installer fetches the release binary, verifies its SHA-256 checksum, installs to `~/.local/bin` with no sudo, and installs no third-party software. You can also read `scripts/install.sh` and run it from a checkout. Setup shows every startup-file patch before writing anything, and `humansh uninstall` actually uninstalls.

**"What does it send to the model?"**
> Your request, shell, OS, arch, a privacy-normalised directory label, and a fixed list of detected tools. Never shell history, environment variables, file contents, or your username. The classifier runs locally, so lines classified as real commands or ambiguous never generate a network call at all.

**"Local models / Ollama?"**
> Not yet — today it's the Codex/Claude/Cursor CLIs or an OpenRouter key. It's a fair ask; tracked in [#18](https://github.com/agenticlab-ai/humansh/issues/18).

**Done (2026-08-27):** the roadmap issues are open — [Ollama/local models #18](https://github.com/agenticlab-ai/humansh/issues/18), [fish #19](https://github.com/agenticlab-ai/humansh/issues/19), [Windows/WSL #20](https://github.com/agenticlab-ai/humansh/issues/20) — so every answer ends with a link instead of a shrug.

---

## Prep checklist (before any more posting)

One-time work, roughly a day, that raises every channel's conversion:

- [ ] **GitHub topics** on the repo: `cli`, `shell`, `zsh`, `bash`, `terminal`, `ai`, `llm`, `developer-tools`, `go`, `natural-language`, `productivity`.
- [ ] **Social preview image** (repo Settings → Social preview, 1280×640): the two-line demo — English in, command out, "Review it, then press Enter."
- [ ] **Demo assets:** the existing `docs/assets/humansh-demo.gif`; plus a 30–60s screen-recorded MP4 (for X and Product Hunt — GIFs autoplay poorly there) showing: `git status` running instantly → an English request → command lands editable → user edits it → Enter. Also record an asciinema for embedding in posts.
- [x] **Roadmap issues** opened for Ollama/local models ([#18](https://github.com/agenticlab-ai/humansh/issues/18)), fish ([#19](https://github.com/agenticlab-ai/humansh/issues/19)), Windows/WSL ([#20](https://github.com/agenticlab-ai/humansh/issues/20)).
- [ ] **F5Bot alerts** (f5bot.com, free) — emails you when keywords appear on HN/Reddit/Lobsters: `humansh`, `copilot cli`, `zsh-codex`, `shell-gpt`, `natural language shell`, `forgot the command`.
- [ ] **A `#humansh` saved search / notifications** on X, Mastodon, Bluesky.
- [x] **Boilerplate file** (copy bank): [marketing/copy-bank.md](marketing/copy-bank.md) — the one-liners, differentiators, objection answers (with roadmap links), plus length-trimmed descriptions and language rules. Every post starts from that file.
- [ ] **Referrer analytics:** check GitHub Insights → Traffic weekly; if humansh.com can log install-script hits and landing referrers, wire that up now so you can tell which channel actually converts.

---

## Channels

Priority order. Space them out — the calendar at the end sequences everything.

### 1. GitHub itself

**Why:** Every channel below dumps people onto the repo; the README converts or loses them. It's already strong. The repo is also a discovery surface on its own (topic pages, GitHub search, trending).

**How/when:**
- Ship the prep checklist above.
- Cut a **tagged release with human release notes** before each launch push — releases are content, and "v0.x: <headline feature>" gives every repost a reason to exist.
- Star velocity in a short window is what puts repos on github.com/trending — another reason to cluster launches (calendar below) instead of dribbling posts out.

**Message:** the README *is* the message. Keep its first screen (tagline + GIF + the 4-line demo block) untouched by feature creep.

### 2. Reddit

**Why:** The highest-signal audiences live in small subs, and Reddit tolerates maker posts far better than HN — if you follow each sub's rules. This is the best place to *sharpen messaging cheaply* before HN round 2.

**How/when:**
- **One sub at a time, 2–3 days apart.** Same-day crossposting gets you flagged as a spammer and the posts compete.
- Weekday mornings US Eastern (14:00–16:00 UTC). Tue–Thu best.
- **Read each sub's self-promo rules first** (sidebar/wiki). Respect the ~10%-self-promo norm by also participating normally (the F5Bot replies count).
- Always a **text post** (not a bare link): GIF or asciinema link at top, short body, repo link, then stay in the comments all day.
- Disclose authorship in the first line ("I built…").

**Where, in order, with ready-to-paste drafts:**

**r/commandline** — the bullseye sub.

> **Title:** humansh — type plain English at your zsh/bash prompt, get a command back to review. Never auto-runs. (MIT, Go)
>
> **Body:** I kept alt-tabbing to a chatbot every time I needed an ffmpeg/find/tar incantation, so I built the thing I wanted: type the English at your normal prompt, and the command lands *in your editable command line* — nothing runs until you press Enter yourself.
>
> The part I'm proudest of isn't the LLM call — it's everything around it: a local, deterministic classifier decides English-vs-command *before any network call*, so `git status` stays instant and offline, and genuinely ambiguous lines are left untouched. Integration is at the ZLE/Readline layer, so `cd`, aliases, functions and job control all behave normally — it's not a wrapper, REPL, or replacement shell. High-risk output (the `rm -rf` genre) is gated behind a second deliberate key sequence. It reuses your existing Codex/Claude Code/Cursor CLI (their auth, their billing), or an OpenRouter key — with no silent fallback to metered billing.
>
> MIT, written in Go, macOS/Linux. Repo: https://github.com/agenticlab-ai/humansh — would love to hear where the classifier gets it wrong for you; you can teach it your own commands and phrasing.

**r/zsh** — go full ZLE-nerd; this sub rewards mechanism, not product.

> **Title:** I built a ZLE "Smart Enter": classifies the buffer as command vs English before accept-line, translates only the English
>
> **Body:** Enter stays Enter for real commands (a local deterministic classifier decides — no network, no LLM), but a line like `show me which process is listening on port 3000` gets translated and re-inserted into the editable buffer for review; a second Enter runs it. Escape cancels and Ctrl-G force-translates; all bindings are captured, restored on uninstall, and collision-reported at setup. Curious what edge cases in your keymaps/widgets would break this — vi-mode users especially. Repo + binding details: https://github.com/agenticlab-ai/humansh

**r/ClaudeAI** (then, spaced by weeks, the Codex and Cursor communities) — lead with differentiator #4.

> **Title:** Your Claude Code subscription can now translate plain English into shell commands, inline in your own terminal (open source)
>
> **Body:** humansh sits inside your existing zsh/bash and uses the `claude` CLI you already have — its auth, its billing, no new API key. Type English at your prompt; the command lands in your editable command line for review, and nothing runs until you press Enter. Real commands are classified locally and never touch the provider, so it costs zero quota until you actually ask for a translation, and each call runs the CLI isolated in an empty temp dir. MIT, Go: https://github.com/agenticlab-ai/humansh

**r/golang** — engineering angle only; ideally link the dev.to deep-dive (channel 6) rather than the repo alone.

> **Title:** humansh: shell-native English→command translation in Go — PTY tests, race detector, and build-enforced module boundaries
>
> **Body:** Open-sourced a Go tool that integrates with zsh/bash at the line-editor layer. The interesting engineering bits: a deterministic text classifier with corpus fixtures instead of an LLM; driving provider CLIs (Codex/Claude/Cursor) as isolated subprocesses in empty temp dirs with minimal env; and a `make verify` gate that runs format, lint, an architecture check that enforces module boundaries, classifier corpus, unit, race, and PTY tests on Ubuntu+macOS. Write-up on the classifier design: <dev.to link>. Repo: https://github.com/agenticlab-ai/humansh

**Later / opportunistic:** r/bash (Readline angle + the Bash 4.3 story), r/opensource (MIT + privacy + contribution ask), r/linux (only with a strong Linux-specific hook; big subs are harsher), r/macapps (the "stop alt-tabbing" consumer angle).

### 3. X/Twitter

**Why:** The AI-dev crowd that pays for Claude Code/Codex/Cursor lives here; demo videos travel; and it compounds — every future release gets a thread.

**How/when:** Launch thread on a Tue–Thu, ~10am ET, MP4 demo (not GIF) on the first tweet. Then ongoing: reply (helpfully, tool mentioned once) to people complaining about forgotten commands or posting chatbot-for-shell workflows; post a mini-thread per release. Never link-dump; the video is the post.

**Launch thread draft:**

> **1/** You know the move: alt-tab to a chatbot, "how do I extract audio from a video with ffmpeg", copy, paste, pray.
>
> I built humansh so you can just type the English at your shell prompt. [MP4 demo]
>
> **2/** The command lands *in your editable command line*. Nothing runs until you press Enter. Risky output (the `rm -rf` genre) needs a second, deliberate keystroke. It never auto-executes. Ever.
>
> **3/** Type a real command and it stays out of the way — a local deterministic classifier decides *before any network call*, so `git status` costs zero latency and zero quota. Not sure what you meant? It refuses to guess and leaves your line untouched.
>
> **4/** No new subscription. It reuses the Codex, Claude Code, or Cursor CLI you already pay for — their auth, their billing. (OpenRouter API key works too, strictly opt-in, no silent fallback to metered billing.)
>
> **5/** It's your actual shell, not a wrapper — zsh/bash at the ZLE/Readline layer, so cd, aliases, functions, job control all just work. And it sends almost nothing: never your history, env vars, files, or username.
>
> **6/** MIT-licensed, single Go binary, macOS/Linux, no sudo, real uninstall.
>
> curl -fsSL https://humansh.com/install | sh
>
> Star it if this is your kind of tool → github.com/agenticlab-ai/humansh

### 4. Mastodon and Bluesky

**Why:** The FOSS/terminal crowd migrated here; hashtags actually work on Mastodon; a maker post from a real account does disproportionately well. Low effort: adapt the X thread.

**How/when:** Same days as the X thread. On Mastodon post from a FOSS-friendly instance (e.g. fosstodon.org) and use hashtags; Bluesky, same text minus hashtags. Boost/engage replies for a day.

**Mastodon draft:**

> Tired of alt-tabbing to a chatbot for ffmpeg/find/awk incantations, I built humansh: type plain English at your zsh/bash prompt, and the command lands in your editable command line — to review, not to run. Nothing executes until you press Enter.
>
> Real commands are classified locally (deterministic, inspectable, no LLM) and never touch a provider. Reuses your existing Codex/Claude Code/Cursor CLI, or an OpenRouter key. Sends almost nothing; MIT; Go; macOS/Linux.
>
> https://github.com/agenticlab-ai/humansh
>
> #CLI #zsh #bash #FOSS #OpenSource #golang #terminal

### 5. Hacker News, round 2

**Why:** Still the single highest-leverage channel for this exact product. The first attempt didn't get "significant attention" (3 points), so HN's own FAQ blesses a repost.

**How/when:**
- **Wait ~3 weeks** from the first post and **tie it to a release** with a headline feature (best candidate: whatever the Reddit rounds demanded most — likely local-model support, which also defuses the top objection).
- **Tue–Thu, 8–11am ET.** Not before dawn US time.
- **Submit the GitHub repo URL**, not humansh.com.
- Title starts `Show HN:` and names the product and the differentiator, under 80 chars.
- Add a first-person text body — Show HN posts with an author comment/story consistently do better.
- Stay at the keyboard for 3–4 hours; answer everything fast and technically. Never ask anyone to upvote, and don't share the direct item link asking for support (HN penalises voting-ring patterns); sharing "I'm on the HN front page" *after* it ranks is fine.

**Title options:**

> Show HN: Humansh – plain English at your shell prompt, review before anything runs

> Show HN: Humansh – my zsh translates English to commands I approve first (MIT, Go)

**Text body draft:**

> I got tired of alt-tabbing to a chatbot every time I forgot an ffmpeg or find incantation, so I built the thing I actually wanted: type the English at your normal prompt, and the command lands in your editable command line. Nothing runs until you press Enter yourself; risky output needs a second deliberate key sequence. It never auto-executes.
>
> The LLM call is the boring 5%. The rest was the hard part, and it's why this isn't a one-line alias: a local, deterministic, inspectable classifier decides English-vs-command before any network call (so `git status` stays instant, offline, and quota-free, and genuinely ambiguous lines are left untouched); integration happens at the ZLE/Readline layer so cd, aliases, functions, and job control behave normally — it's not a wrapper, REPL, or replacement terminal; and each provider CLI runs isolated in an empty temp dir with a minimal environment.
>
> It reuses whichever of Codex/Claude Code/Cursor you already pay for — their auth, their billing, no new API key. OpenRouter is the opt-in metered path, and there's no silent fallback to it. It sends your request plus coarse context (shell, OS, arch, a privacy-normalised directory label, detected tools) — never history, env vars, file contents, or your username.
>
> MIT, Go, macOS/Linux. Since the last time I posted it, it's gained <headline feature>. I'd especially love feedback on the classifier — the rules and weights are documented, and you can teach it your own commands and phrasing.

### 6. Dev.to / long-form technical posts

**Why:** Deep-dives are the content HN/Reddit *can't* call marketing, they rank in search for years, daily.dev syndicates dev.to automatically, and each article gives r/golang and newsletters something legitimate to link.

**How/when:** One article every 2–3 weeks, published on dev.to (canonical) and cross-linked from the repo. Each one ends with the same two lines: what humansh is + repo link.

**Article queue (in order):**
1. **"English or a command? Classifying shell input deterministically — no LLM"** — the evidence rules, weights, corpus fixtures, why `find all files modified today` parses as valid syntax but isn't a command, and why refusing to guess is a feature.
2. **"Making Enter smart: integrating with zsh at the ZLE layer without breaking anyone's shell"** — capturing/restoring bindings, collision reporting, why Bash's Readline can't do conditional Enter and what to do instead.
3. **"Treating AI CLIs as untrusted subprocesses"** — empty temp dirs, minimal env, mandatory isolation flags, failing closed when a flag is rejected, and the no-silent-fallback billing rule.

### 7. Product Hunt

**Why:** Broadest generalist-dev reach, a permanent listing with SEO value, and a badge/traffic spike that feeds GitHub trending. Works best *after* messaging is battle-tested — hence week 4.

**How/when:** Launch at **12:01am PT, a Tuesday or Wednesday**. Prepare: gallery (the MP4 demo first, then 3–4 stills of the two-Enter flow, provider table, privacy bullets), topics (Open Source, Developer Tools, Terminal, Artificial Intelligence), and a maker comment posted immediately at launch. Rally nobody-in-particular: genuine early users leaving reviews matter more than a vote push.

**Name:** humansh
**Tagline (≤60 chars):** `Plain English in your shell. Review before anything runs.`
**Description:** Type what you need at your normal zsh/bash prompt. humansh writes the command into your editable command line — nothing runs until you press Enter. Real commands are detected locally and stay instant. Uses your existing Codex, Claude Code, or Cursor subscription. Open source (MIT).

**Maker comment draft:**

> Hey PH! I built humansh because I was alt-tabbing to a chatbot ten times a day for commands I half-remember. Now I type the English where I already am. Three design rules I refused to break: (1) it never auto-executes — you always review, and risky commands need a second deliberate keystroke; (2) your real commands never touch an AI — a local deterministic classifier keeps `git status` instant and quota-free; (3) no new bill — it drives the Codex/Claude Code/Cursor CLI you already pay for. Free, MIT, macOS/Linux. Ask me anything, especially where the classifier gets it wrong.

### 8. Tool directories

**Why:** Zero-maintenance evergreen discovery; these rank in "X alternative" searches forever.

**How/when:** One afternoon, week 0–1.
- **Terminal Trove** (terminaltrove.com) — curated terminal-tool directory with a submission form; exactly this audience.
- **AlternativeTo** — create the humansh listing and add it as an alternative to GitHub Copilot CLI, Warp, zsh-codex, shell-gpt.
- **LibHunt / awesome-go web mirrors** — mostly automatic once the repo has topics and stars; verify it appears.

**Message:** reuse the default one-liner + the five differentiators, trimmed to each form's field limits.

### 9. Newsletters

**Why:** One inclusion in a good dev newsletter outperforms a week of social posts, and submissions are free. Lead times are 1–4 weeks — submit early (week 1), regardless of where the calendar stands.

**Targets:** Console.dev (console.dev — curated dev-tool newsletter, has a tool-submission form; their reviews are respected), TLDR (tldr.tech), Golang Weekly (golangweekly.com — submit as a Go project), Changelog News (changelog.com — also picks up trending repos automatically), Terminal Trove's newsletter (comes with the directory submission).

**Pitch template (adapt per outlet):**

> Subject: humansh — open-source, review-first English→shell-command translation inside zsh/bash
>
> Hi — I built humansh (MIT, Go, macOS/Linux): type plain English at your normal shell prompt and the command lands in your editable command line — nothing runs until you press Enter. A local deterministic classifier keeps real commands instant and offline, and it reuses the Codex/Claude Code/Cursor CLI your readers likely already pay for, with no new API key. Repo: https://github.com/agenticlab-ai/humansh · 60-second demo: <link>. Thought it might fit <newsletter>'s tools section. Happy to answer anything.

### 10. Awesome lists

**Why:** Steady long-tail GitHub traffic and an implicit quality signal.

**How/when:** Week 2+, one PR each, following each list's CONTRIBUTING rules exactly (some require the project be 30+ days old or have CI/coverage — humansh's CI badge and test suite help): `awesome-shell`, `awesome-zsh-plugins`, `awesome-cli-apps`, `awesome-go` (strictest bar), plus any curated Claude Code / AI-CLI ecosystem lists.

**Message (list entry):** `humansh - Type plain English at your prompt; a reviewed, editable shell command comes back. Never auto-executes.`

### 11. Package managers as discovery

**Why:** `brew search`, AUR votes, and nixpkgs are how a large slice of the audience *finds* tools, not just installs them — and "brew install humansh" in every future post lowers friction.

**How/when:** Start with a **Homebrew tap** (`agenticlab-ai/homebrew-tap`) now — trivial to maintain, and the README/posts can offer it alongside the curl installer. Submit to **homebrew-core** once the repo has the notability it requires (stars/age thresholds). An **AUR** package reaches vocal early adopters cheaply. **nixpkgs** when there's contributor energy. Each addition is itself a small announcement ("humansh is now in homebrew-core") worth a post.

### 12. Comment marketing (evergreen)

**Why:** The steadiest channel of all: people ask "how do I stop googling shell commands" somewhere every week. One genuinely helpful reply that mentions humansh once, with disclosure, converts better than any launch post — and keeps your accounts' self-promo ratio healthy.

**How/when:** Ongoing, driven by the F5Bot alerts from the prep checklist. Rules: answer the actual question first; mention humansh once, with "I built" disclosure; never in threads about a competitor's launch (bad form); skip threads where it'd be a stretch.

### 13. Lower priority: Lobsters, LinkedIn, YouTube, podcasts

- **Lobsters (lobste.rs):** perfect audience, but invite-only and currently AI-tool-skeptical. Only worth it via an existing member, framed around the classifier/ZLE write-up (article #1 or #2), tagged `cli`/`go`, expecting sharp questions. Don't lead with "AI".
- **LinkedIn:** one build-in-public post per milestone (launch, homebrew-core, 1k stars). Reuses the X thread's first two tweets as prose. Low cost, occasionally finds you contributors and dayjob-credibility, rarely users.
- **YouTube:** only if you enjoy making videos; otherwise the 60s MP4 embedded elsewhere is enough. Alternative: offer the demo to terminal-tool YouTubers/streamers as material.
- **Podcasts:** after some traction (a few hundred stars), pitch Go-ecosystem and dev-tool shows (the Changelog family, Console's podcast) with the "treating AI CLIs as untrusted subprocesses" story — a stronger hook for hosts than the product itself.

---

## Launch calendar

First HN post was Thu 2026-08-27. All Reddit/HN slots are 9–11am US Eastern.

| When | Channel | Action |
|---|---|---|
| **Week 0** (now – Aug 30) | GitHub, directories, newsletters | Prep checklist: topics, social image, MP4/asciinema, roadmap issues, F5Bot. Submit Terminal Trove + AlternativeTo. Send newsletter pitches (long lead). Create Homebrew tap. |
| **Week 1** Tue Sep 1 | r/commandline | Text post (draft above). Camp in comments all day. |
| Wed Sep 2 | X + Mastodon/Bluesky | Launch thread + fediverse post. |
| Thu Sep 3 | r/zsh | ZLE-angle post. |
| **Week 2** Tue Sep 8 | r/ClaudeAI | Subscription-angle post. (Codex/Cursor subs in later weeks, spaced.) |
| Wed Sep 9 | dev.to | Publish article #1 (classifier deep-dive). |
| Thu Sep 10 | r/golang | Engineering post linking article #1. |
| Fri–Sun | Awesome lists | PRs to awesome-shell, awesome-zsh-plugins, awesome-cli-apps. |
| **Week 3** by Mon Sep 14 | GitHub | Cut the release with the headline feature (fold in week-1/2 feedback). |
| Tue Sep 15, ~9am ET | **Hacker News** | Show HN round 2: repo URL, new title, text body, 4 hours of comment duty. |
| Thu Sep 17 | r/bash or r/opensource | Whichever fits the week's momentum. |
| **Week 4** Tue Sep 22, 12:01am PT | **Product Hunt** | Launch + maker comment; X/Mastodon amplify same morning. |
| Wed–Fri | LinkedIn, remaining subs | Milestone post; r/macapps / r/linux if there's a hook. |
| **Ongoing** | Comment marketing, dev.to, releases | F5Bot replies weekly; article every 2–3 weeks; every release = short X/Mastodon post; each packaging milestone = mini-announcement. |

Rule of thumb: never two big-channel launches in the same week; always ship something (release, article) between them so each post has news.

---

## Rules of engagement

1. **The first 90 minutes decide the post.** Ranking on HN/Reddit is driven by early engagement — only post when you can respond fast for 3–4 hours.
2. **Always disclose.** "I built this" in the first line, everywhere. Maker posts are welcome; stealth marketing gets you banned and screenshotted.
3. **Never solicit votes** — no upvote asks, no direct HN item links in DMs/groupchats. Both platforms detect ring behavior and penalize the account *and* the domain.
4. **Concede fair points fast.** "You're right, it doesn't do X — here's the issue tracking it" wins more users than any rebuttal. Thank critics; nothing defuses a thread like the author being pleasant.
5. **Every repeated question is a docs bug.** If it's asked twice, the answer goes into the README/FAQ, and future copy preempts it (that's how this file was built).
6. **Claims stay documented.** If a line in a post can't be pasted from README/docs, soften it or document it first. The privacy and never-auto-execute claims are trust-critical — never round them up.
7. **One channel per day, at most.** Attention spikes need you present; simultaneous posts split you and look like a campaign.
8. **Feedback loops beat broadcasts.** The point of weeks 1–2 is to harvest objections cheaply and fix messaging (and the product) before the channels that only give you one shot (HN front page, PH).

---

## What to measure

Weekly, in a simple log (date, channel, link, numbers):

- **GitHub:** stars (and star *velocity* on launch days), Insights → Traffic (unique visitors, referrers — this tells you which channel actually converts), release download counts.
- **Site:** humansh.com visits and `/install` hits by referrer, if logging is wired up.
- **Per post:** upvotes/points, comments, and — more useful — which objections appeared and whether the draft answered them.
- **Leading indicator that matters most:** issues and discussions opened by strangers. A stranger filing a classifier edge case is worth more than 50 stars.

After week 4, keep whichever two channels drove real referrer traffic, drop the rest to opportunistic, and settle into the evergreen loop: release → short posts → article → comment marketing.
