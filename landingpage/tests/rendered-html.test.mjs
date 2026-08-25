import assert from "node:assert/strict";
import test from "node:test";

async function request(path = "/", method = "GET") {
  const workerUrl = new URL("../dist/server/index.js", import.meta.url);
  workerUrl.searchParams.set("test", `${process.pid}-${Date.now()}-${path}`);
  const { default: worker } = await import(workerUrl.href);

  return worker.fetch(
    new Request(`http://localhost${path}`, {
      method,
      headers: { accept: "text/html" },
    }),
    {
      ASSETS: {
        fetch: async () => new Response("Not found", { status: 404 }),
      },
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
  assert.match(html, /<title>HumanSH — Stay in your terminal<\/title>/i);
  assert.match(visibleHtml, /Forgot the command\? Stay in the terminal\./);
  assert.match(visibleHtml, /curl -fsSL https:\/\/humansh\.com\/install \| bash/);
  assert.match(visibleHtml, /aria-label="Copy install command"/);
  assert.doesNotMatch(visibleHtml, /raw\.githubusercontent\.com/);
  assert.equal(visibleHtml.match(/\bfree\b/gi)?.length, 2);

  const meteredMentions = visibleHtml.match(/.{0,40}metered API.{0,80}/gi) ?? [];
  assert.ok(meteredMentions.length > 0);
  for (const mention of meteredMentions) {
    assert.match(mention, /OpenRouter/i);
  }
});

test("redirects the branded install URL to the canonical installer", async () => {
  const response = await request("/install");
  assert.equal(response.status, 307);
  assert.equal(
    response.headers.get("location"),
    "https://raw.githubusercontent.com/agenticlab-ai/humansh/main/scripts/install.sh",
  );
});
