"use client";

import type { ClientWebsiteEvent } from "../lib/website-events";

const eventEndpoint = "/api/website-events";

export function trackWebsiteEvent(event: ClientWebsiteEvent) {
  const body = JSON.stringify(event);

  try {
    if (typeof navigator.sendBeacon === "function") {
      const queued = navigator.sendBeacon(
        eventEndpoint,
        new Blob([body], { type: "application/json" }),
      );

      if (queued) {
        return;
      }
    }

    void fetch(eventEndpoint, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body,
      credentials: "same-origin",
      keepalive: true,
    }).catch(() => {
      // Analytics must never interrupt the visitor's action.
    });
  } catch {
    // Analytics must never interrupt the visitor's action.
  }
}
