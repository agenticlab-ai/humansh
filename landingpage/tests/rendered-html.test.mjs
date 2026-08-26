import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import test from "node:test";

let requestSequence = 0;

async function request(path = "/", options = {}) {
  const {
    method = "GET",
    headers = {},
    body,
    analytics,
  } = options;
  const workerUrl = new URL("../dist/server/index.js", import.meta.url);
  requestSequence += 1;
  workerUrl.searchParams.set("test", `${process.pid}-${Date.now()}-${requestSequence}`);
  const { default: worker } = await import(workerUrl.href);

  return worker.fetch(
    new Request(`http://localhost${path}`, {
      method,
      headers: { accept: "text/html", ...headers },
      body,
    }),
    {
      ASSETS: {
        fetch: async () => new Response("Not found", { status: 404 }),
      },
      ...(analytics ? { HUMANSH_ANALYTICS: analytics } : {}),
    },
    {
      waitUntil() {},
      passThroughOnException() {},
    },
  );
}

test("server-renders the HumanSH landing page", async () => {
  const response = await request();
  assert.equal(response.status, 200);
  assert.match(response.headers.get("content-type") ?? "", /^text\/html\b/i);

  const html = await response.text();
  const visibleHtml = html.replace(/<script\b[^>]*>[\s\S]*?<\/script>/gi, "");
  const headMatch = html.match(/<head>([\s\S]*?)<\/head>/i);
  assert.ok(headMatch, "expected a document head");
  const headHtml = headMatch[1];
  const afterHeadHtml = html.slice((headMatch.index ?? 0) + headMatch[0].length);

  assert.match(html, /<title>HumanSH — Plain English in your terminal<\/title>/i);
  assert.match(
    headHtml,
    /<link[^>]+rel="shortcut icon"[^>]+href="\/favicon\.ico\?v=2"[^>]*>/i,
  );
  assert.match(
    headHtml,
    /<link[^>]+rel="icon"[^>]+href="\/favicon-32\.png\?v=2"[^>]*>/i,
  );
  assert.match(
    headHtml,
    /<link[^>]+rel="apple-touch-icon"[^>]+href="\/apple-touch-icon\.png\?v=2"[^>]*>/i,
  );
  assert.doesNotMatch(
    afterHeadHtml,
    /<link[^>]+(?:favicon|apple-touch-icon)[^>]*>/i,
  );
  assert.match(
    visibleHtml,
    /Use plain English without changing how you work\./,
  );
  assert.match(visibleHtml, /Keep the terminal you know/);
  assert.match(visibleHtml, /Same terminal · 3 steps/);
  assert.match(visibleHtml, /Do I need to switch terminal apps\?/);
  assert.match(visibleHtml, /iTerm, Terminal\.app, VS Code(?:&#x27;|')s terminal/);
  assert.match(visibleHtml, /curl -fsSL https:\/\/humansh\.com\/install \| sh/);
  assert.match(visibleHtml, /aria-label="Copy install command"/);
  assert.doesNotMatch(visibleHtml, /raw\.githubusercontent\.com/);
  assert.equal(visibleHtml.match(/\bfree\b/gi)?.length, 2);
  assert.equal(
    visibleHtml.match(/data-analytics-event="github_open"/g)?.length,
    8,
  );
  assert.match(
    visibleHtml,
    /Website counts aggregate visits and selected actions without cookies or user IDs/,
  );

  const meteredMentions = visibleHtml.match(/.{0,40}metered API.{0,80}/gi) ?? [];
  assert.ok(meteredMentions.length > 0);
  for (const mention of meteredMentions) {
    assert.match(mention, /OpenRouter/i);
  }

  const wranglerConfig = JSON.parse(
    await readFile(new URL("../dist/server/wrangler.json", import.meta.url), "utf8"),
  );
  assert.deepEqual(wranglerConfig.analytics_engine_datasets, [
    {
      binding: "HUMANSH_ANALYTICS",
      dataset: "humansh_web_events",
    },
  ]);

  const favicon = await readFile(
    new URL("../dist/client/favicon-32.png", import.meta.url),
  );
  assert.equal(favicon.readUInt32BE(16), 32);
  assert.equal(favicon.readUInt32BE(20), 32);

  const fallbackFavicon = await readFile(
    new URL("../dist/client/favicon.ico", import.meta.url),
  );
  assert.deepEqual([...fallbackFavicon.subarray(0, 8)], [0, 0, 1, 0, 1, 0, 32, 32]);
});

test("redirects the branded install URL to the canonical installer", async () => {
  const points = [];
  const response = await request("/install", {
    analytics: {
      writeDataPoint(point) {
        points.push(point);
      },
    },
  });

  assert.equal(response.status, 307);
  assert.equal(
    response.headers.get("location"),
    "https://raw.githubusercontent.com/agenticlab-ai/humansh/main/scripts/install.sh",
  );
  assert.deepEqual(points, [
    {
      indexes: ["localhost:install_request:install"],
      blobs: ["install_request", "install", "localhost"],
      doubles: [1],
    },
  ]);
});

test("records allowlisted website actions without visitor identifiers", async () => {
  const points = [];
  const response = await request("/api/website-events", {
    method: "POST",
    headers: {
      "Content-Type": "application/json",
      Origin: "http://localhost",
      "Sec-Fetch-Site": "same-origin",
    },
    body: JSON.stringify({ event: "github_open", placement: "hero_repo" }),
    analytics: {
      writeDataPoint(point) {
        points.push(point);
      },
    },
  });

  assert.equal(response.status, 204);
  assert.equal(response.headers.get("cache-control"), "no-store");
  assert.deepEqual(points, [
    {
      indexes: ["localhost:github_open:hero_repo"],
      blobs: ["github_open", "hero_repo", "localhost"],
      doubles: [1],
    },
  ]);
});

test("rejects cross-site and unknown website events", async () => {
  const points = [];
  const analytics = {
    writeDataPoint(point) {
      points.push(point);
    },
  };

  const crossSiteResponse = await request("/api/website-events", {
    method: "POST",
    headers: {
      "Content-Type": "application/json",
      Origin: "https://example.com",
      "Sec-Fetch-Site": "cross-site",
    },
    body: JSON.stringify({ event: "install_copy", placement: "install" }),
    analytics,
  });
  assert.equal(crossSiteResponse.status, 403);

  for (const event of ["install_request", "__proto__"]) {
    const unknownEventResponse = await request("/api/website-events", {
      method: "POST",
      headers: {
        "Content-Type": "application/json",
        Origin: "http://localhost",
      },
      body: JSON.stringify({ event, placement: "install" }),
      analytics,
    });
    assert.equal(unknownEventResponse.status, 400);
  }

  assert.deepEqual(points, []);
});
