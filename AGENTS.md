# Development Instructions

- Use Go (Golang) for the `humansh` binary and all core application logic.
- Use Zsh only for the ZLE integration and Bash only for the Readline integration specified in `build_with_ai.md`.
- Use portable POSIX `sh` only for the installer, uninstaller, release, and architecture-check scripts specified in `build_with_ai.md`.
- Whenever fixing a reported issue, including one reported again after an earlier fix, add an end-to-end regression test that demonstrates the original failure and prevents it from recurring.
