# Setup

`humansh setup` is a guided review, not a silent application of defaults. It shows every change it intends to make and writes nothing until you confirm. This document covers the full flow; the [README](../README.md) has the short version.

## Installing

### From a release

```sh
curl -fsSL https://raw.githubusercontent.com/agenticlab-ai/humansh/main/scripts/install.sh | sh
```

The installer downloads the matching binary from the latest GitHub release and verifies its SHA-256 checksum before installing to `~/.local/bin`. It never installs Codex, Claude Code, Cursor, Homebrew, Go, or any other third-party software on your behalf.

A fork can point the installer at its own release repository:

```sh
curl -fsSL https://raw.githubusercontent.com/OWNER/REPOSITORY/main/scripts/install.sh \
  | HUMANSH_REPOSITORY=OWNER/REPOSITORY sh
```

See [security](security.md) for why a same-host checksum establishes integrity but not authenticity.

### From a checkout

```sh
./scripts/install.sh --local
```

The installer replaces the binary atomically, and restores the previous binary if interactive setup does not complete.

## Choosing shells

Setup detects and configures every supported shell on the machine, so you do not need to know or select your current one. To restrict an installation to a single shell:

```sh
./scripts/install.sh --local --shell bash
humansh setup --shell bash          # or afterwards, against the installed binary
```

Setup verifies Zsh and Bash independently, installs the embedded integration for each usable shell under the XDG data directory, and adds one idempotent managed block to each applicable startup file.

### Bash version floor

Bash integration requires **Bash 4.3 or newer**, so humansh can safely capture and restore existing Readline shell-command bindings. macOS still ships Bash 3.2; install a current one with `brew install bash`.

The compatibility report lists each installed supported shell with its version. For an unsupported Bash it shows the 4.3 minimum and continues with Zsh. Rerun `humansh setup` after installing a current Bash to add its integration.

### Shell modes

Zsh supports Smart Enter, where one key classifies the line. Bash uses explicit translation, because Readline cannot safely make Enter conditionally accept or replace the buffer.

Each managed block exports the resolved binding values before sourcing its immutable, hashed shell asset, so changing a binding never modifies the asset itself.

## The interactive flow

The **shell compatibility** phase lists only installed supported shells and concise versions. Startup-file activation details are held back for the final review, where you see only the managed-block patches for applicable files — never unrelated shell configuration.

Slow discovery and provider checks show an in-place loader on a terminal. Redirected output gets one stable `Checking…` line instead of animation. `NO_COLOR=1` disables styling; `--yes` runs setup deterministically without prompts.

Before writing anything, setup shows shell modes, provider and model, directory-context privacy, timeout, and shortcuts. At a shortcut prompt, type a readable value such as `Ctrl-G`, `Ctrl-X Ctrl-T`, or `Esc t`.

Setup preserves startup-file symlinks, applies all shell changes transactionally, and refuses to apply a stale reviewed patch. Pressing Ctrl-C at a prompt or during any shell, provider, key, or model check exits setup with status 130, restores normal terminal input, and leaves credentials, configuration, and shell files unchanged.

## Providers during setup

Interactive setup always shows a compact four-provider menu and asks what to use. A previously saved provider is only the default answer, not a silent choice. Troubleshooting detail stays hidden for providers you do not select.

The menu uses non-inference discovery: it checks whether each CLI executable exists without calling optional login, status, version, or help commands. After you choose Codex, Claude Code, or Cursor, setup discloses and sends one fixed minimal prompt through a fresh isolated subprocess. That live check may consume a small amount of provider quota.

Authentication belongs to the selected CLI distribution. Humansh neither infers its billing mode nor starts a login flow. This supports centrally managed corporate distributions whose inference command works while login subcommands are intentionally disabled. If the probe fails, setup shows the provider's bounded, redacted error text and waits while you fix the issue. Press Enter to retry the same provider in place, or answer no to return to the provider list.

If several Claude or Cursor CLI installations are present in `PATH`, setup lets you keep automatic selection or pin one exact executable. Shell aliases and the Cursor editor launcher are intentionally not used.

Setup requires **one live, responding provider**. With none selected it stops before writing credentials, configuration, or shell integration. The minimal probe verifies provider reachability; run `humansh provider test NAME` to verify the complete production structured-output invocation and all mandatory safety flags.

### OpenRouter

Interactive setup configures OpenRouter in place; the standalone `humansh provider configure openrouter` remains available for changing the model later. Both flows:

1. Accept the key without echo and validate it through the read-only key-status endpoint.
2. Use read-only model metadata to require `structured_outputs`, not merely basic `response_format` support. Incompatible models are rejected before any model credits are spent, with a link to OpenRouter's filtered compatible-model list.
3. Run one minimal metered request against humansh's exact strict-output schema. This is the only billed step, and it is disclosed before it runs.
4. Stage the result until final confirmation.

Paste the next model ID directly into the repeated model prompt — there is no intervening yes/no question, and `back` returns to the AI-provider menu. An already-valid key is not rechecked for each model attempt.

If the compatibility check fails, setup stops rather than saving an unusable configuration. Changing the model manually with `humansh config set` does **not** mark it proven; use guided setup or `provider configure` for that.

## Uninstalling

```sh
humansh uninstall            # removes integrations, keeps config and credentials
humansh uninstall --purge    # also removes them, after explicit confirmation
```

Declining the purge confirmation cancels the entire uninstall without changing any files. Use `--yes` only for an intentional non-interactive purge. From a checkout, `sh scripts/uninstall.sh [--purge]` and `make uninstall` do the same.

A child process cannot alter its parent shell, so restart a shell only if it already has humansh loaded in memory.
