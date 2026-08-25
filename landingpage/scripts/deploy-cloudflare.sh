#!/bin/sh

set -eu

SCRIPT_DIR=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
PROJECT_DIR=$(dirname "$SCRIPT_DIR")
WORKER_NAME="humansh"
DRY_RUN=false

usage() {
  printf '%s\n' "Usage: $0 [--dry-run]"
}

case "${1:-}" in
  "")
    ;;
  --dry-run)
    DRY_RUN=true
    ;;
  -h|--help)
    usage
    exit 0
    ;;
  *)
    usage >&2
    exit 2
    ;;
esac

if [ "$#" -gt 1 ]; then
  usage >&2
  exit 2
fi

if [ "$DRY_RUN" = false ] && [ -z "${CLOUDFLARE_ACCOUNT_ID:-}" ]; then
  printf '%s\n' "CLOUDFLARE_ACCOUNT_ID is not set." >&2
  printf '%s\n' "Export it in your shell before deploying." >&2
  exit 1
fi

if ! command -v npm >/dev/null 2>&1; then
  printf '%s\n' "npm is required to deploy the landing page." >&2
  exit 1
fi

cd "$PROJECT_DIR"

mkdir -p .wrangler
export WRANGLER_LOG_PATH="${WRANGLER_LOG_PATH:-$PROJECT_DIR/.wrangler/wrangler.log}"

printf '%s\n' "Installing locked dependencies..."
npm ci

printf '%s\n' "Checking the landing page..."
npm run lint
npm test

printf '%s\n' "Deploying Cloudflare Worker: $WORKER_NAME"
if [ "$DRY_RUN" = true ]; then
  ./node_modules/.bin/wrangler deploy \
    --config dist/server/wrangler.json \
    --name "$WORKER_NAME" \
    --dry-run
else
  ./node_modules/.bin/wrangler deploy \
    --config dist/server/wrangler.json \
    --name "$WORKER_NAME"
fi
