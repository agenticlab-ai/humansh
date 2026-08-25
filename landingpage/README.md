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

## Website Analytics

Cloudflare Web Analytics supplies aggregate page views. The Worker also writes
these first-party actions to the `humansh_web_events` Analytics Engine dataset:

- `install_copy`: the install command was copied successfully
- `install_request`: the branded `/install` URL was requested with `GET`
- `github_open`: a link to the HumanSH repository, docs, or security model was opened

Each data point contains only the event, its placement on the page, the site
hostname, and a count. It does not contain cookies, IP addresses, user IDs,
referrers, user agents, or command text. The binding is included in the generated
Wrangler configuration, and Cloudflare creates the dataset on its first deployed
write.

Example 30-day summary query:

```sql
SELECT
  blob1 AS event,
  blob2 AS placement,
  SUM(_sample_interval * double1) AS total
FROM humansh_web_events
WHERE timestamp > NOW() - INTERVAL '30' DAY
  AND blob3 = 'humansh.com'
GROUP BY blob1, blob2
ORDER BY total DESC
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
