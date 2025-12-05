import { Show } from "solid-js";

interface ErrorInfo {
  phase: string;
  summary: string | null;
  exitCode: number | null;
}

interface TigerButtonProps {
  isBuilding: boolean;
  hasError: boolean;
  isDisconnected: boolean;
  errorInfo: ErrorInfo | null;
  onClick: () => void;
}

const PHASE_LABELS: Record<string, string> = {
  prebuild: "Prebuild Error",
  build: "Build Error",
  runtime: "Runtime Error",
};

/** Extract a one-line summary from error output */
function extractErrorSummary(error: string, phase: string): string | null {
  const lines = error.trim().split("\n").filter(l => l.trim());

  if (phase === "build") {
    // Match Go compiler error: ./file.go:line:col: message
    const goErrorMatch = error.match(/\.\/([^:]+):(\d+):\d+:\s*(.+)/);
    if (goErrorMatch) {
      const [, file, line, message] = goErrorMatch;
      const shortMsg = message.length > 40 ? message.slice(0, 37) + "..." : message;
      return `${file}:${line} ${shortMsg}`;
    }
  }

  if (phase === "runtime") {
    // For runtime, get the last log line (most relevant)
    // Strip Go log timestamp: 2025/11/29 15:26:08 message
    const lastLine = lines[lines.length - 1] || "";
    const logMatch = lastLine.match(/^\d{4}\/\d{2}\/\d{2}\s+\d{2}:\d{2}:\d{2}\s+(.+)/);
    if (logMatch) {
      const msg = logMatch[1];
      return msg.length > 50 ? msg.slice(0, 47) + "..." : msg;
    }
    // Check for panic
    const panicMatch = error.match(/^panic:\s*(.+)/m);
    if (panicMatch) {
      const msg = panicMatch[1];
      return msg.length > 50 ? msg.slice(0, 47) + "..." : msg;
    }
  }

  // Fallback: first non-empty line if short enough
  const firstLine = lines[0];
  if (firstLine && firstLine.length < 60) {
    return firstLine;
  }

  return null;
}

export function TigerButton(props: TigerButtonProps) {
  const showRunning = () => props.isBuilding && !props.isDisconnected;
  const showExpanded = () => props.hasError && !props.isDisconnected && props.errorInfo;

  const phaseLabel = () => {
    if (!props.errorInfo) return "Error";
    return PHASE_LABELS[props.errorInfo.phase] || "Error";
  };

  const summarySuffix = () => {
    // For runtime errors, show exit code instead of log snippet
    if (props.errorInfo?.phase === "runtime" && props.errorInfo.exitCode !== null) {
      return `exit ${props.errorInfo.exitCode}`;
    }
    return props.errorInfo?.summary ?? null;
  };

  return (
    <button
      class="tygor-tiger-btn"
      classList={{
        "tygor-tiger-btn--building": showRunning(),
        "tygor-tiger-btn--error": props.hasError && !props.isDisconnected,
        "tygor-tiger-btn--disconnected": props.isDisconnected,
        "tygor-tiger-btn--expanded": showExpanded(),
      }}
      onClick={props.onClick}
      title={props.isDisconnected ? "Tygor DevTools (disconnected)" : "Open Tygor DevTools"}
    >
      <span class="tygor-tiger-btn-icon">
        <Show when={showRunning()} fallback={<span class="tygor-tiger-icon">🐯</span>}>
          <span class="tygor-tiger-runner">🐅</span>
        </Show>
      </span>
      <Show when={showExpanded()}>
        <span class="tygor-tiger-btn-content">
          <span class="tygor-tiger-btn-title">{phaseLabel()}</span>
          <Show when={summarySuffix()}>
            <span class="tygor-tiger-btn-summary">{summarySuffix()}</span>
          </Show>
        </span>
      </Show>
    </button>
  );
}

export { extractErrorSummary };
