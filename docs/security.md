# Security model

The core invariant is simple: an LLM-generated command is never automatically executed. Providers return untrusted data, Go validates and risk-scores it, and the ZLE or Readline integration inserts it into the editable parent-shell buffer. Only the user's later keypress delegates the reviewed command to the parent Zsh or Bash process. The binary never calls `eval` or runs generated commands.

The threat model includes malicious or malformed provider output, prompt injection inside the user's request, compromised project instructions, inherited credential overrides, stale/metered authentication, terminal-control injection, shell-output obfuscation, and local configuration corruption. It does not make a compromised workstation, provider account, release host, or reviewed command safe.

With the default `working_context="basename"`, provider request data is limited to the English request, selected target shell, OS/architecture, a privacy-normalized working-directory label, and a fixed allowlist of installed tools. The home directory and a basename equal to the username become `~`. `working_context="none"` omits the directory; the explicit `full` setting sends its full path. Shell history, environment variables, credentials, files, repository contents, directory listings, hostnames, usernames, and command output are not sent.

CLI providers run with explicit argv, stdin-only dynamic requests, a private isolated working directory containing only humansh-owned schema/output files, bounded output, a hard timeout, process-group cancellation, and a minimal environment. Codex shell/unified-exec controls are mandatory. Claude tools and customizations are disabled. Cursor runs in documented read-only Ask mode with sandboxing inside an empty disposable workspace; its CLI does not currently expose native schema enforcement or a no-tools flag, so schema enforcement remains local and the documentation does not claim all provider-managed read-only capabilities are removed. OpenRouter uses direct HTTPS and strict structured output. Credentials are redacted from normal and debug errors, including credential-loader failures.

Provider output is rejected if it is malformed, multiline, overlong, contains terminal/Unicode controls, presentation prompts, Markdown, surrounding prose, invalid target-shell syntax, or rejected obfuscation. Exit 25 means incomplete/malformed provider output. Exit 26 means an extracted value was rejected by local policy.

High-risk generated commands—recursive deletion, disk destruction, download-and-execute, destructive Git/infrastructure/database operations, encoded execution, and similar patterns—remain editable but ordinary Enter refuses them in both integrations. Editing reruns local analysis, so adding whitespace cannot bypass the gate. Risk inspection uses a portable AST where possible and conservative fallback checks for target-shell forms the portable parser cannot understand.

OpenRouter keys are stored in macOS Keychain when available or a mode-0600 file inside a mode-0700 directory. The runtime API host is pinned to OpenRouter, credentialed requests do not follow redirects, and malformed credential controls are rejected before HTTP. The read-only current-key and model-capability checks do not consume model credits; the latter rejects models that advertise `response_format` without `structured_outputs`. After those checks pass, setup discloses and automatically runs one minimal metered request to prove humansh's exact strict-output schema. Provider error details are JSON-decoded, credential-redacted, stripped of terminal controls, and length-bounded before display.

`curl ... | sh` is intentionally classified high risk, including humansh's convenience installer and provider vendor installers. For stronger review, download the script, inspect it, then run it separately.

A SHA-256 checksum fetched from the same release host provides integrity against accidental corruption, not authenticity against a compromised host. The current release script does not yet sign checksums or emit a provenance attestation; release documentation must not claim otherwise.

Humansh does not create cloud accounts, purchase credits or subscriptions, silently switch providers, or collect telemetry. Provider account creation and billing remain explicit actions between the user and the provider.

Report vulnerabilities using the repository's private security-advisory mechanism. No security-contact email is asserted here because none is configured in the repository.
