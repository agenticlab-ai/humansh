#!/bin/sh
set -eu

output=$(go list -f '{{.ImportPath}}|{{join .Imports " "}}' ./internal/...)
failed=0

while IFS='|' read -r package imports; do
  case "$package" in
	*/internal/cli)
	  case " $imports " in
		*"/internal/llm/codex "*|*"/internal/llm/claude "*|*"/internal/llm/cursor "*|*"/internal/llm/openrouter "*|*"/internal/shell/zsh "*|*"/internal/shell/bash "*)
		  echo "CLI bypasses the composition root with a concrete adapter import in $package" >&2
		  failed=1
		  ;;
	  esac
	  ;;
    */internal/app)
      case " $imports " in
        *"/internal/llm/codex "*|*"/internal/llm/claude "*|*"/internal/llm/cursor "*|*"/internal/llm/openrouter "*|*"/internal/shell/zsh "*|*"/internal/shell/bash "*)
          echo "forbidden concrete adapter import in $package" >&2
          failed=1
          ;;
      esac
      ;;
    */internal/llm|*/internal/llm/*)
      case " $imports " in
        *"/internal/shell "*|*"/internal/shell/"*) echo "forbidden shell import in $package" >&2; failed=1 ;;
      esac
	  case "$package" in
		*/internal/llm/codex|*/internal/llm/claude|*/internal/llm/cursor|*/internal/llm/openrouter)
		  case " $imports " in
			*"/internal/config "*) echo "provider adapter bypasses bootstrap/config injection in $package" >&2; failed=1 ;;
		  esac
		  ;;
	  esac
      ;;
    */internal/shell|*/internal/shell/*)
      case " $imports " in
        *"/internal/llm "*|*"/internal/llm/"*)
          echo "forbidden provider import in $package" >&2
          failed=1
          ;;
      esac
      ;;
  esac
done <<EOF
$output
EOF

exit "$failed"
