export const clientWebsiteEventPlacements = {
  install_copy: ["install"],
  github_open: [
    "header_repo",
    "hero_repo",
    "privacy_security",
    "install_guide",
    "install_repo",
    "footer_docs",
    "footer_security",
    "footer_repo",
  ],
} as const;

export type ClientWebsiteEventName = keyof typeof clientWebsiteEventPlacements;

export type ClientWebsiteEvent = {
  [EventName in ClientWebsiteEventName]: {
    event: EventName;
    placement: (typeof clientWebsiteEventPlacements)[EventName][number];
  };
}[ClientWebsiteEventName];

export function isClientWebsiteEvent(value: unknown): value is ClientWebsiteEvent {
  if (!value || typeof value !== "object") {
    return false;
  }

  const payload = value as Record<string, unknown>;
  if (typeof payload.event !== "string" || typeof payload.placement !== "string") {
    return false;
  }

  if (!Object.hasOwn(clientWebsiteEventPlacements, payload.event)) {
    return false;
  }

  const placements = clientWebsiteEventPlacements[
    payload.event as ClientWebsiteEventName
  ] as readonly string[] | undefined;

  return placements?.includes(payload.placement) ?? false;
}
