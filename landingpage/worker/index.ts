/** Cloudflare Worker entry point for the HumanSH landing page. */
import { handleImageOptimization, DEFAULT_DEVICE_SIZES, DEFAULT_IMAGE_SIZES } from "vinext/server/image-optimization";
import handler from "vinext/server/app-router-entry";
import { isClientWebsiteEvent } from "../lib/website-events";

const maxEventBodyBytes = 256;

interface Env {
  ASSETS: Fetcher;
  DB: D1Database;
  HUMANSH_ANALYTICS?: AnalyticsEngineDataset;
  IMAGES: {
    input(stream: ReadableStream): {
      transform(options: Record<string, unknown>): {
        output(options: { format: string; quality: number }): Promise<{ response(): Response }>;
      };
    };
  };
}

interface ExecutionContext {
  waitUntil(promise: Promise<unknown>): void;
  passThroughOnException(): void;
}

type WebsiteEventName = "github_open" | "install_copy" | "install_request";

function recordWebsiteEvent(
  env: Env,
  url: URL,
  event: WebsiteEventName,
  placement: string,
) {
  if (!env.HUMANSH_ANALYTICS) {
    return;
  }

  try {
    env.HUMANSH_ANALYTICS.writeDataPoint({
      indexes: [`${url.hostname}:${event}:${placement}`],
      blobs: [event, placement, url.hostname],
      doubles: [1],
    });
  } catch {
    // A metrics failure must never affect the website or installer.
  }
}

function eventResponse(status: number) {
  return new Response(null, {
    status,
    headers: {
      "Cache-Control": "no-store",
      ...(status === 405 ? { Allow: "POST" } : {}),
    },
  });
}

async function handleWebsiteEvent(request: Request, env: Env, url: URL) {
  if (request.method !== "POST") {
    return eventResponse(405);
  }

  const origin = request.headers.get("Origin");
  const fetchSite = request.headers.get("Sec-Fetch-Site");
  if ((origin && origin !== url.origin) || fetchSite === "cross-site") {
    return eventResponse(403);
  }

  if (!request.headers.get("Content-Type")?.startsWith("application/json")) {
    return eventResponse(415);
  }

  const contentLength = Number(request.headers.get("Content-Length"));
  if (Number.isFinite(contentLength) && contentLength > maxEventBodyBytes) {
    return eventResponse(413);
  }

  const body = await request.text();
  if (new TextEncoder().encode(body).byteLength > maxEventBodyBytes) {
    return eventResponse(413);
  }

  let payload: unknown;
  try {
    payload = JSON.parse(body);
  } catch {
    return eventResponse(400);
  }

  if (!isClientWebsiteEvent(payload)) {
    return eventResponse(400);
  }

  recordWebsiteEvent(env, url, payload.event, payload.placement);
  return eventResponse(204);
}

// Image security config. SVG sources with .svg extension auto-skip the
// optimization endpoint on the client side (served directly, no proxy).
// To route SVGs through the optimizer (with security headers), set
// dangerouslyAllowSVG: true in next.config.js and uncomment below:
// const imageConfig: ImageConfig = { dangerouslyAllowSVG: true };

const worker = {
  async fetch(request: Request, env: Env, ctx: ExecutionContext): Promise<Response> {
    const url = new URL(request.url);

    if (url.pathname === "/api/website-events") {
      return handleWebsiteEvent(request, env, url);
    }

    if (url.pathname === "/_vinext/image") {
      const allowedWidths = [...DEFAULT_DEVICE_SIZES, ...DEFAULT_IMAGE_SIZES];
      return handleImageOptimization(request, {
        fetchAsset: (path) => env.ASSETS.fetch(new Request(new URL(path, request.url))),
        transformImage: async (body, { width, format, quality }) => {
          const result = await env.IMAGES.input(body).transform(width > 0 ? { width } : {}).output({ format, quality });
          return result.response();
        },
      }, allowedWidths);
    }

    if (url.pathname === "/install" && request.method === "GET") {
      recordWebsiteEvent(env, url, "install_request", "install");
    }

    return handler.fetch(request, env, ctx);
  },
};

export default worker;
