import { Alert } from "./Alert";
import type { TygorRpcError } from "./types";

interface RpcErrorProps {
  error: TygorRpcError;
  onDismiss: () => void;
}

export function RpcError(props: RpcErrorProps) {
  const title = () => `RPC Error`;

  return (
    <Alert title={title()} onDismiss={props.onDismiss} timestamp={props.error.timestamp}>
      <div class="rpc-error">
        <div class="rpc-error-endpoint">
          {props.error.service}.{props.error.method}
        </div>
        <div class="rpc-error-details">
          <span class="rpc-error-code">{props.error.code}</span>
          <span class="rpc-error-message">{props.error.message}</span>
        </div>
      </div>
    </Alert>
  );
}
