import { Show, JSX, createSignal, onCleanup } from "solid-js";

function formatDuration(ms: number): string {
  const seconds = Math.floor(ms / 1000);
  if (seconds < 60) return `${seconds}s`;
  const minutes = Math.floor(seconds / 60);
  if (minutes < 60) return `${minutes}m`;
  return `${Math.floor(minutes / 60)}h`;
}

interface AlertProps {
  /** Title shown after [tygor] prefix - optional */
  title?: string;
  /** Body content - optional */
  children?: JSX.Element;
  /** Called when dismiss button clicked */
  onDismiss?: () => void;
  /** Additional action buttons */
  actions?: JSX.Element;
  /** Timestamp when alert started (Date.now()) - shows elapsed time in header */
  timestamp?: number;
}

export function Alert(props: AlertProps) {
  const hasHeader = () => props.title || props.onDismiss || props.actions || props.timestamp;

  // Tick signal to force duration recalculation every second
  const [tick, setTick] = createSignal(0);
  const interval = setInterval(() => setTick((t) => t + 1), 1000);
  onCleanup(() => clearInterval(interval));

  const duration = () => {
    tick(); // Subscribe to tick updates
    if (!props.timestamp) return null;
    return formatDuration(Date.now() - props.timestamp);
  };

  return (
    <div class="tygor-alert">
      <Show when={hasHeader()}>
        <div class="tygor-alert-header">
          <div class="tygor-alert-title">
            <span class="tygor-alert-icon">🐯</span>
            <span class="tygor-alert-prefix">tygor</span>
            <Show when={props.title}>
              <span class="tygor-alert-title-text">{props.title}</span>
            </Show>
          </div>
          <div class="tygor-alert-actions">
            <Show when={duration()}>
              <span class="tygor-alert-duration">{duration()}</span>
            </Show>
            {props.actions}
            <Show when={props.onDismiss}>
              <button class="tygor-alert-close" onClick={props.onDismiss} title="Dismiss">×</button>
            </Show>
          </div>
        </div>
      </Show>
      <Show when={props.children}>
        <div class="tygor-alert-body">
          {props.children}
        </div>
      </Show>
    </div>
  );
}
