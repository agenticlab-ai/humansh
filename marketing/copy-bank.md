# Copy bank

Canonical marketing language for humansh. Every public post, listing, pitch, and reply starts from this file — copy, trim, adapt the frame, but don't re-invent the claims. If a claim isn't documented in the README or docs, it doesn't belong here (and doesn't go in a post).

Companion to the launch playbook ([marketing.md](../marketing.md)), which says *where and when* to use this language.

---

## Product facts

| | |
|---|---|
| Name | `humansh` in dev-community posts and technical writing (matches the README and CLI); **HumanSH** on consumer-facing surfaces (humansh.com, Product Hunt) |
| What it is | Type plain English at your shell prompt. Get a real command back — to review, not to run. |
| Install | `curl -fsSL https://humansh.com/install \| sh` *(canonical form — the site and the POSIX installer use `sh`)* |
| License / language | MIT · Go · single binary, no sudo, checksum-verified install, clean uninstall |
| Platforms | macOS and Linux, `arm64`/`amd64` · Zsh (Smart Enter) and Bash 4.3+ (explicit translation) |
| Providers | Codex, Claude Code, or Cursor CLIs (their auth, their billing) · OpenRouter API key as the explicit, opt-in metered path — never a silent fallback |
| Links | [humansh.com](https://humansh.com) · [Repo](https://github.com/agenticlab-ai/humansh) · [Security model](https://github.com/agenticlab-ai/humansh/blob/main/docs/security.md) · Roadmap: [Ollama #18](https://github.com/agenticlab-ai/humansh/issues/18), [fish #19](https://github.com/agenticlab-ai/humansh/issues/19), [Windows/WSL #20](https://github.com/agenticlab-ai/humansh/issues/20) |

---

## One-liners — pick per context

| Context | Line |
|---|---|
| Default | Type plain English at your shell prompt. Get a real command back — to review, not to run. |
| Trigger moment | The moment you'd alt-tab to a chatbot for an ffmpeg/find/awk incantation — just type the English where you already are. |
| Against the crowd | Not a new terminal, not a REPL, not an agent, not a wrapper. Your own zsh/bash, with one new trick. |
| Subscription hook | Already pay for Claude Code, Codex, or Cursor? humansh reuses that CLI. No new API key, no new bill. |
| Safety first | It never runs anything. Every generated command lands in your editable prompt; nothing executes until you press Enter yourself. |

---

## Descriptions by length

For directory forms, newsletter pitches, and bios — pre-trimmed so field limits never force improvised wording.

**Tagline (≤60 chars):**

> Plain English in your shell. Review before anything runs.

**Form one-liner (~130 chars):**

> Type plain English at your zsh/bash prompt — a reviewed, editable command comes back. Never auto-executes. MIT, Go, macOS/Linux.

**Awesome-list entry:**

> humansh - Type plain English at your prompt; a reviewed, editable shell command comes back. Never auto-executes.

**Short paragraph (~55 words):**

> Type what you need at your normal zsh/bash prompt. humansh writes the command into your editable command line — nothing runs until you press Enter. Real commands are detected locally and stay instant. Uses your existing Codex, Claude Code, or Cursor subscription. Open source (MIT).

**Pitch paragraph (~90 words, newsletter/outreach):**

> humansh (MIT, Go, macOS/Linux): type plain English at your normal shell prompt and the command lands in your editable command line — nothing runs until you press Enter. A local deterministic classifier keeps real commands instant and offline, and it reuses the Codex, Claude Code, or Cursor CLI you already pay for, with no new API key. OpenRouter's metered API is the explicit opt-in path, never a silent fallback. Repo: https://github.com/agenticlab-ai/humansh

---

## The five differentiators, ranked

Lead with 1–3 for terminal audiences, 4 for AI-tool audiences, 5 for the FOSS/privacy crowd.

1. **Never auto-executes.** Commands are inserted for review; high-risk output needs a second, deliberate key sequence.
2. **Runs in your shell, not a wrapper.** Integration at the ZLE/Readline layer — `cd`, `export`, aliases, functions, job control all behave normally.
3. **Stays quiet when you type real commands.** A local, deterministic, inspectable classifier (not an LLM) decides before any network call. `git status` costs no latency and no quota. Genuinely ambiguous input is left untouched.
4. **Uses your existing provider CLI.** Codex, Claude Code, or Cursor own auth and billing. OpenRouter metered billing is opt-in only, with no silent fallback.
5. **Sends very little, verifiably.** Your request, shell, OS, arch, a privacy-normalised directory label, detected tools. Never history, env vars, file contents, or your username. Secrets live in the OS keychain.

---

## Objection answers

Paste-adapt, never argue. Concede the fair part first.

**"This could be a one-line alias to ollama/sgpt."**

> Totally — for the LLM call, it could. The call is the easy 5%. The rest is the product: a deterministic local classifier so `git status` never touches a provider and ambiguous input is left alone; ZLE/Readline integration so the result lands *editable in your real prompt* instead of stdout; a risk gate so `rm -rf` output needs a deliberate second keystroke; and each provider CLI runs isolated in an empty temp dir with a minimal environment. That's also why the repo isn't one file.

**"Atuin / history search already does this."**

> Different job: Atuin finds commands you've already run — it's great, use both. humansh writes commands you've never run (or can never remember the flags for).

**"How is this different from GitHub Copilot CLI / Warp / zsh-codex?"**

> Copilot CLI is a separate `gh copilot` invocation with its own subscription; Warp is a replacement terminal. humansh lives inside your existing zsh/bash at the line-editor layer, never auto-executes, decides English-vs-command locally before any network call, and reuses whichever of Codex/Claude Code/Cursor you already pay for.

**"curl | bash, seriously?"**

> The installer fetches the release binary, verifies its SHA-256 checksum, installs to `~/.local/bin` with no sudo, and installs no third-party software. You can also read `scripts/install.sh` and run it from a checkout. Setup shows every startup-file patch before writing anything, and `humansh uninstall` actually uninstalls. (humansh even classifies `curl | sh` patterns as high-risk in its own risk gate — including its own installer.)

**"What does it send to the model?"**

> Your request, shell, OS, arch, a privacy-normalised directory label, and a fixed list of detected tools. Never shell history, environment variables, file contents, or your username. The classifier runs locally, so lines classified as real commands or ambiguous never generate a network call at all.

**"Local models / Ollama?"**

> Not yet — today it's the Codex/Claude/Cursor CLIs or an OpenRouter key. It's a fair ask, and it's the top roadmap item: [#18](https://github.com/agenticlab-ai/humansh/issues/18).

**"fish?"**

> Not yet — Zsh and Bash 4.3+ today. fish's `commandline` builtin makes the review-first flow look very tractable; tracked in [#19](https://github.com/agenticlab-ai/humansh/issues/19).

**"Windows?"**

> Officially macOS and Linux today. WSL2 is expected to mostly work but is untested and undocumented so far; native Windows (PowerShell) is a much larger design. Both tracked in [#20](https://github.com/agenticlab-ai/humansh/issues/20) — comments from Windows users directly shape the ordering.

---

## Language rules

1. **Claims must be copy-pasteable from the README or docs.** If it isn't documented, soften it or document it first — the privacy and never-auto-execute claims are trust-critical and never get rounded up.
2. **Privacy wording:** "sends very little" plus the documented list. Never "sends nothing" or "no data leaves your machine" — a translation request does leave.
3. **Execution wording:** humansh *inserts commands for review*; it never "runs", "executes", or "does" anything. The user runs commands.
4. **Install command:** always `curl -fsSL https://humansh.com/install | sh` (the site's and installer's form).
5. **Disclose authorship** ("I built…") in the first line of every post, everywhere.
6. **Name the product in titles** — a title that could describe any AI shell tool is a wasted title.
