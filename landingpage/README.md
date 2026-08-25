# HumanSH Landing Page

The public website for [HumanSH](https://humansh.com). It introduces the
product, explains its privacy model and provider options, and serves the stable
`https://humansh.com/install` installer URL.

The site runs on [Vinext](https://github.com/cloudflare/vinext) and deploys to a
Cloudflare Worker named `humansh`.

## Prerequisites

- Node.js `>=22.13.0`
- npm

## Local Development

Install the locked dependencies and start the development server:

```sh
npm ci
npm run dev
```

The main product page lives in `app/page.tsx`. The branded installer endpoint
in `app/install/route.ts` redirects to the canonical installer maintained in
the repository root.

## Validation

```sh
npm run lint
npm test
```

The test command creates a production build and verifies the rendered landing
page, the controlled use of product claims, and the `/install` redirect.

To validate the complete Cloudflare package without publishing it:

```sh
npm run deploy:cloudflare -- --dry-run
```

## Cloudflare Deployment

Authenticate Wrangler once:

```sh
npx wrangler login
```

Export `CLOUDFLARE_ACCOUNT_ID` in your shell, then deploy after changing the
site:

```sh
npm run deploy:cloudflare
```

The deployment script restores the locked dependencies, runs lint and tests,
creates a fresh production build, and deploys the generated Worker
configuration. The `humansh.com` custom domain remains attached to the
`humansh` Worker across deployments.

## Project Structure

- `app/`: landing page, layout metadata, global styles, and route handlers
- `public/`: demo and social-preview assets
- `tests/`: rendered-output and installer-route checks
- `scripts/deploy-cloudflare.sh`: repeatable production deployment
- `worker/`: Cloudflare Worker entry point
