# Providers

The selected provider is explicit in `config.toml`. `humansh` never silently changes providers and never falls back from a subscription provider to metered OpenRouter.

The fake-CLI compatibility fixtures use Codex CLI 0.148.x/0.149.x, Claude Code 2.1.238, and Cursor CLI 2026.07.23 as reviewed interface baselines, not exact-version pins. Claude Code compatibility starts at 2.1.169, the first official release with `--safe-mode`, and still requires every mandatory isolation flag to pass its direct capability probe. Cursor is capability-gated because its calendar-style version is less useful than its installed command surface.

Interactive setup always asks which provider to use, with the saved choice shown only as the default. The menu contains one compact status per provider; executable, version, authentication, and recovery details are shown only for the selected provider. Claude's compact status says `Fresh CLI logged out` when an already-running session may still work but the new process humansh must launch cannot authenticate. Setup offers the selected CLI's official login—`codex login`, `claude auth login --claudeai`, or `cursor-agent login`. The official process remains attached directly to the terminal; humansh does not proxy credentials, OAuth codes, cookies, or browser state. Existing metered/override authentication is explained before that explicit confirmation, and humansh never logs it out automatically.

Setup does not install shell integration without one proven, ready provider. If the selected provider cannot be qualified and the user declines another choice, setup exits nonzero before saving credentials, configuration, or shell files. An invoking installer therefore rolls back its binary replacement instead of reporting an unusable installation as complete.

## Codex

The Codex adapter accepts only saved ChatGPT subscription authentication. It checks `codex login status`, corroborates it with the local Codex auth record, rejects API-key evidence, and provides a narrow explicit confirmation path for changed status wording.

If status wording changes while the auth record still corroborates ChatGPT, inspect the diagnostic and explicitly confirm with:

```sh
humansh provider configure codex --confirm-subscription-auth
```

API-key evidence always wins and cannot be overridden by this confirmation.

Every translation runs in a private empty directory with a minimal environment. The invocation uses an exact argument allowlist, read-only sandboxing, `approval_policy="never"`, ephemeral history, ignored user/project rules, and mandatory `features.shell_tool=false` plus `features.unified_exec=false`. Read-only sandboxing is a backstop; it does not disable shell execution by itself.

Non-billable diagnostics gate on the banner from the actual `codex exec --version` subcommand—not a potentially different launcher banner—and check that `codex exec --help` advertises every mandatory non-interactive flag. The resulting capabilities are recorded in `doctor --json`. This is not a behavioral proof of config semantics. The complete strict settings are enforced on every translation, any rejected flag/config fails closed, and the account-backed isolation test below is required before qualifying a release.

Codex has no supported maximum-turn setting in the tested versions. `humansh` parses only the completed `--output-last-message` file and ignores schema-shaped intermediate stdout. A final `ok` object without a command is a neutral, retryable exit-25 incomplete response.

Before qualifying a Codex CLI version for release, run the opt-in behavioral isolation test with an authenticated ChatGPT subscription. It may consume subscription quota and is intentionally skipped in ordinary CI:

```sh
HUMANSH_REAL_CODEX_ISOLATION=1 \
HUMANSH_REAL_CODEX_MODEL=MODEL \
go test ./internal/llm/codex -run TestRealCodexBehavioralIsolation -count=1
```

Repeat it for every supported CLI version. The test places a distinctive tripwire in the private working directory, baits the model to inspect it, and verifies that mandatory shell-tool isolation prevents the secret from appearing in the completed response.

Because `--ignore-user-config` bypasses the user's model selection, an unset humansh model uses Codex's built-in default. Copy an explicit model with:

```sh
humansh config set providers.codex.model MODEL
```

## Claude Code

The Claude adapter accepts only a claude.ai subscription login. It rejects Console/API, Bedrock, Vertex, Foundry, custom-base-URL, and environment credential overrides. Claude's documented `CLAUDE_CODE_OAUTH_TOKEN`, `CLAUDE_CODE_OAUTH_REFRESH_TOKEN`, and `CLAUDE_CODE_OAUTH_SCOPES` subscription variables are narrowly forwarded only to the Claude subprocess; their values are never printed or persisted. Diagnostics and translations also receive `HOME`, absolute Claude/Anthropic/XDG credential-storage roots, and the non-secret macOS user identity fields needed to locate the same Keychain/config login as a direct `claude auth status`; unrelated inherited variables remain excluded. Non-billable diagnosis uses the configured `providers.claude.binary`, then the first `claude` selected by `PATH`, and finally the native installer's fixed `~/.local/bin/claude` location so a newly installed or self-updated CLI remains visible before the parent shell refreshes PATH. Only after the user selects Claude, if multiple executables named `claude` are present in `PATH`, interactive setup lists them and lets the user retain automatic selection or pin one absolute path; it never treats a shell alias as the executable. It uses the selected binary's actual print-mode banner from `claude -p --version`, requires Claude Code 2.1.169 or later for `--safe-mode`, and probes the mandatory help surface. Recovery and interactive-login commands name the exact diagnosed executable rather than an alias or another installation. Because some versions omit accepted options from help, `--max-turns 3` is verified with a non-model parser probe (`claude -p --max-turns 3 --help`) instead of grepping for that flag. Translation uses safe mode, `--tools ""` to remove normal built-ins, an explicit `mcp__*` denial, no Chrome, no slash commands, no session persistence, and a three-turn cap so Claude's structured-output workflow can finish. It deliberately does not use a blanket `*` deny because that also removes Claude's synthetic `StructuredOutput` mechanism, and it does not use `--bare` because bare mode bypasses normal subscription credentials.

To pin an installation outside setup, or restore automatic PATH selection:

```sh
humansh config set providers.claude.binary /absolute/path/to/claude
humansh config set providers.claude.binary auto
```

Repair authentication with:

```sh
claude auth login --claudeai
claude auth status --text
```

## Cursor CLI

The Cursor adapter uses the account-backed browser login reported by `cursor-agent status --format json`. Automatic executable selection prefers `cursor-agent` and falls back to the legacy-compatible `agent` name; it never invokes the `cursor` editor launcher. If separate installations are present, setup can pin one absolute path. To change it later:

```sh
humansh config set providers.cursor.binary /absolute/path/to/cursor-agent
humansh config set providers.cursor.binary auto
```

Cursor also supports explicit API keys and custom API endpoints. Humansh rejects `CURSOR_API_KEY`, `CURSOR_AUTH_TOKEN`, and `CURSOR_API_ENDPOINT` for this provider so selecting a subscription CLI cannot silently change billing or redirect credentials. Sign in and verify the exact CLI with:

```sh
cursor-agent login
cursor-agent status
humansh provider test cursor
```

Translation runs with `--print --output-format json --mode ask --sandbox enabled --trust` from a new mode-0700 empty directory. `--trust` applies only to that disposable empty workspace and prevents an interactive workspace-trust prompt. Ask mode is Cursor's documented read-only mode; unlike Codex and Claude, Cursor does not currently expose a native no-tools flag or JSON-schema output flag. Humansh therefore supplies the canonical schema and request together on stdin, asks Cursor not to inspect external state, extracts only the final JSON result envelope, and applies the same strict local JSON and semantic validation used for every provider. The empty workspace prevents project files and project rules from being discovered, but Cursor may still have provider-managed read-only capabilities; humansh does not claim they are removed.

An optional model can be chosen during setup or with:

```sh
humansh config set providers.cursor.model MODEL
```

## OpenRouter

OpenRouter is explicitly metered. Choose it from interactive `humansh setup` to configure it without leaving the wizard. Setup points to `https://openrouter.ai/settings/keys` for creating a key and `https://openrouter.ai/models` for copying a concrete `provider/model` ID, reads the key without echo, and stages the credential and settings until the final review is confirmed. The separate `humansh provider configure openrouter --model provider/model` command remains available for later model changes. Humansh stores the key in macOS Keychain when possible, otherwise in a dedicated mode-0600 credentials file. `OPENROUTER_API_KEY` has highest precedence and is never persisted.

After OpenRouter and a concrete model are explicitly selected, configuration first checks the model's read-only catalog metadata. It requires the explicit `structured_outputs` capability; `response_format` alone is insufficient. Models that fail this free check never receive a completion request. Interactive setup immediately repeats the model-ID prompt so the next `provider/model` value can be pasted directly; it does not put a yes/no prompt between model attempts. Enter `back` to return to the provider menu. The key is validated once and is not rechecked for every candidate model. A model that passes metadata receives one disclosed, automatic, minimal metered request with the real strict structured-output schema. The probe uses one short message and caps output at 128 tokens; only the concrete model slug that passes is saved. `openrouter/auto` is not accepted as a runtime default. Requests omit both `tools` and `tool_choice`; the wire schema omits unsupported `$schema` and `maxLength`, while local validation retains all bounds.

Before the metered probe, humansh calls `GET /api/v1/key` to validate the key and `GET /api/v1/model/{author}/{slug}` to inspect the model without using model credits. Completion requests opt into OpenRouter router metadata; safe provider error text and routing summaries are retained so a 404 caused by endpoint filtering is not mislabeled as a nonexistent model. Persisted `structured_output_proven` and `structured_output_model` fields tie the proof to the exact chosen model; the config command clears both on a model change, a stale manual mismatch is rejected, and runtime translation fails closed until configure succeeds again. The configured base URL is pinned to `https://openrouter.ai/api/v1` so the credential cannot be redirected to a different host.

## Measured cost and latency

Release documentation must record measurements using the table below. Real account-backed measurements have not yet been run in this checkout; do not treat the pre-implementation review values as a performance promise.

The reproducible Codex measurement gate runs five fixed, non-sensitive requests by default through the production provider adapter, diagnosis, structured-response decoding, and local response/command validation. It accepts 3–20 samples. Token fields remain `unavailable` because the isolated final-message interface does not expose usage accounting:

```sh
HUMANSH_REAL_CODEX_MEASUREMENTS=1 \
HUMANSH_REAL_CODEX_MODEL=MODEL \
HUMANSH_REAL_CODEX_SAMPLES=5 \
go test ./internal/llm/codex -run TestRealCodexReleaseMeasurements -count=1 -v
```

| Provider/model | Client version | Samples | Input/prompt tokens | Output tokens | Total tokens | p50 | p95 | Date |
|---|---|---:|---:|---:|---:|---:|---:|---|
| Codex | pending account-backed release test | — | — | — | — | — | — | — |
| Claude | pending account-backed release test | — | — | — | — | — | — | — |
| Cursor | pending account-backed release test | — | — | — | — | — | — | — |
| OpenRouter | pending configured-model release test | — | — | — | — | — | — | — |

The specification review recorded one Codex warning baseline at roughly 7,507 total tokens and several seconds on 0.148/0.149. It was not produced by this release checkout, lacks p50/p95 sampling, and is not a release measurement.
