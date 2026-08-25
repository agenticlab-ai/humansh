"use client";

import { useEffect, useRef, useState } from "react";

type CopyState = "idle" | "copied" | "error";

function copyWithFallback(text: string) {
  const textarea = document.createElement("textarea");
  textarea.value = text;
  textarea.setAttribute("readonly", "");
  textarea.style.position = "fixed";
  textarea.style.opacity = "0";
  textarea.style.pointerEvents = "none";
  document.body.appendChild(textarea);
  textarea.select();

  const copied = document.execCommand("copy");
  textarea.remove();

  if (!copied) {
    throw new Error("Copy failed");
  }
}

async function copyToClipboard(text: string) {
  if (navigator.clipboard && window.isSecureContext) {
    try {
      await navigator.clipboard.writeText(text);
      return;
    } catch {
      // Some browsers expose the Clipboard API but deny access. Use the
      // selection-based fallback before reporting an error to the user.
    }
  }

  copyWithFallback(text);
}

export function CopyInstallButton({ command }: { command: string }) {
  const [state, setState] = useState<CopyState>("idle");
  const resetTimer = useRef<ReturnType<typeof setTimeout> | null>(null);

  useEffect(
    () => () => {
      if (resetTimer.current) {
        clearTimeout(resetTimer.current);
      }
    },
    [],
  );

  async function handleCopy() {
    if (resetTimer.current) {
      clearTimeout(resetTimer.current);
    }

    try {
      await copyToClipboard(command);
      setState("copied");
    } catch {
      setState("error");
    }

    resetTimer.current = setTimeout(() => setState("idle"), 2200);
  }

  const label =
    state === "copied" ? "Copied" : state === "error" ? "Try again" : "Copy";

  return (
    <button
      type="button"
      className="copyInstallButton"
      data-state={state}
      onClick={handleCopy}
      aria-label={state === "copied" ? "Install command copied" : "Copy install command"}
    >
      <span
        className={`copyInstallIcon${state === "copied" ? " isCopied" : ""}`}
        aria-hidden="true"
      >
        {state === "copied" ? "✓" : null}
      </span>
      <span aria-live="polite">{label}</span>
    </button>
  );
}
