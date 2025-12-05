import { Alert } from "./Alert";

interface StatusIndicatorProps {
  message: string;
  timestamp: number | null;
  onDismiss: () => void;
}

export function StatusIndicator(props: StatusIndicatorProps) {
  return (
    <Alert onDismiss={props.onDismiss} timestamp={props.timestamp ?? undefined}>
      <div class="status-message">{props.message}</div>
    </Alert>
  );
}
