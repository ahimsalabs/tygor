import { createSignal, Show } from "solid-js";
import { Alert } from "./Alert";
import type { GetStatusResponse } from "../devserver/types";

const PHASE_LABELS: Record<string, string> = {
  prebuild: "Prebuild",
  build: "Build",
  runtime: "Runtime",
};

interface OverlayProps {
  status: GetStatusResponse & { status: "error" };
  onDismiss: () => void;
  timestamp?: number;
}

export function Overlay(props: OverlayProps) {
  const [copied, setCopied] = createSignal(false);

  const title = () => `Go ${PHASE_LABELS[props.status.phase] || "Build"} Error`;

  const copyText = () => {
    const { command, cwd, error } = props.status;
    return (command ? `${cwd}$ ${command}\n\n` : "") + error.trim();
  };

  const handleCopy = async () => {
    await navigator.clipboard.writeText(copyText());
    setCopied(true);
    setTimeout(() => setCopied(false), 1500);
  };

  const copyButton = (
    <button
      class={`tygor-btn ${copied() ? "tygor-btn-copied" : ""}`}
      onClick={handleCopy}
      title="Copy error"
    >
      {copied() ? "Copied" : "Copy"}
    </button>
  );

  return (
    <Alert title={title()} onDismiss={props.onDismiss} actions={copyButton} timestamp={props.timestamp}>
      <Show when={props.status.command}>
        <p class="command">$ {props.status.command}</p>
      </Show>
      <pre>{props.status.error}</pre>
      <p class="hint">Fix the error and save — auto-reloads when fixed.</p>
    </Alert>
  );
}
