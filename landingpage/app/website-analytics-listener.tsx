"use client";

import { useEffect } from "react";
import { isClientWebsiteEvent } from "../lib/website-events";
import { trackWebsiteEvent } from "./website-analytics";

export function WebsiteAnalyticsListener() {
  useEffect(() => {
    function handleClick(event: MouseEvent) {
      if (!(event.target instanceof Element)) {
        return;
      }

      const link = event.target.closest<HTMLAnchorElement>(
        "a[data-analytics-event][data-analytics-placement]",
      );
      if (!link) {
        return;
      }

      const websiteEvent = {
        event: link.dataset.analyticsEvent,
        placement: link.dataset.analyticsPlacement,
      };

      if (isClientWebsiteEvent(websiteEvent)) {
        trackWebsiteEvent(websiteEvent);
      }
    }

    document.addEventListener("click", handleClick, { capture: true });
    return () => document.removeEventListener("click", handleClick, { capture: true });
  }, []);

  return null;
}
