import type { Error as TygorErrorEnvelope, ErrorCode } from "./generated/types.js";

// Re-export generated types
export type { ErrorCode } from "./generated/types.js";

/**
 * TygorError is the base class for all tygor client errors.
 * Use instanceof to narrow to ServerError or TransportError.
 */
export abstract class TygorError extends Error {
  abstract readonly kind: "server" | "transport";

  constructor(message: string, options?: ErrorOptions) {
    super(message, options);
  }
}

/**
 * ServerError represents an application-level error returned by the tygor server.
 * These have a structured code, message, and optional details.
 */
export class ServerError extends TygorError {
  readonly kind = "server" as const;
  code: ErrorCode;
  details?: Record<string, unknown> | null;
  httpStatus: number;

  constructor(code: ErrorCode, message: string, httpStatus: number, details?: Record<string, unknown> | null) {
    super(message);
    this.name = "ServerError";
    this.code = code;
    this.httpStatus = httpStatus;
    this.details = details;
  }
}

/**
 * TransportError represents a transport-level error (proxy, network, malformed response).
 * These occur when the response is not a valid tygor envelope.
 */
export class TransportError extends TygorError {
  readonly kind = "transport" as const;
  httpStatus: number;
  rawBody?: string;

  constructor(message: string, httpStatus: number, cause?: unknown, rawBody?: string) {
    super(message, { cause });
    this.name = "TransportError";
    this.httpStatus = httpStatus;
    this.rawBody = rawBody;
  }
}

/**
 * Response is the discriminated union type for service responses.
 * Use this with the "never throw" client pattern:
 *
 * @example
 * const response = await client.Users.Get.safe({ id });
 * if (response.error) {
 *   console.log(response.error.code);
 * } else {
 *   console.log(response.result.name);
 * }
 */
export type Response<T> =
  | { result: T; error?: never }
  | { result?: never; error: TygorErrorEnvelope };

/**
 * Empty represents a void response (null).
 */
export type Empty = null;

export type FetchFunction = (url: string, init?: RequestInit) => Promise<globalThis.Response>;

/**
 * Standard Schema interface (v1) for runtime validation.
 * Compatible with Zod, Valibot, ArkType, and other conforming libraries.
 * @see https://standardschema.dev/
 */
export interface StandardSchema<Input = unknown, Output = Input> {
  readonly "~standard": {
    readonly version: 1;
    readonly vendor: string;
    readonly validate: (value: unknown) => StandardSchemaResult<Output> | Promise<StandardSchemaResult<Output>>;
  };
}

/** Result of Standard Schema validation */
export type StandardSchemaResult<T> = StandardSchemaSuccess<T> | StandardSchemaFailure;

/** Successful validation result */
export interface StandardSchemaSuccess<T> {
  readonly value: T;
  readonly issues?: undefined;
}

/** Failed validation result */
export interface StandardSchemaFailure {
  readonly issues: readonly StandardSchemaIssue[];
}

/** Validation issue from Standard Schema */
export interface StandardSchemaIssue {
  readonly message: string;
  readonly path?: readonly (PropertyKey | { readonly key: PropertyKey })[];
}

/**
 * ValidationError is thrown when client-side schema validation fails.
 */
export class ValidationError extends Error {
  readonly kind = "validation" as const;
  issues: readonly StandardSchemaIssue[];
  direction: "request" | "response";
  endpoint: string;

  constructor(endpoint: string, direction: "request" | "response", issues: readonly StandardSchemaIssue[]) {
    const paths = issues.map((i) => i.path?.join(".") || "(root)").join(", ");
    super(`${direction} validation failed for ${endpoint}: ${paths}`);
    this.name = "ValidationError";
    this.endpoint = endpoint;
    this.direction = direction;
    this.issues = issues;
  }
}

/**
 * Schema map entry for a single endpoint.
 */
export interface SchemaMapEntry {
  request: StandardSchema;
  response: StandardSchema;
}

/**
 * Schema map type - maps endpoint names to their request/response schemas.
 */
export type SchemaMap = Record<string, SchemaMapEntry>;

/**
 * Validation configuration.
 */
export interface ValidateConfig {
  /** Validate request data before sending. Default: true if schemas provided */
  request?: boolean;
  /** Validate response data after receiving. Default: false */
  response?: boolean;
}

export interface ClientConfig {
  baseUrl?: string;
  headers?: () => Record<string, string>;
  fetch?: FetchFunction;
  /** Schema map for client-side validation. Import from schemas.map.ts */
  schemas?: SchemaMap;
  /** Validation options. Only applies if schemas is provided */
  validate?: ValidateConfig;
  /**
   * Whether to emit 'tygor:rpc-error' events for devtools to display.
   * Default: true. Set to false for internal/infrastructure clients
   * where errors are handled separately (e.g., devtools' own API client).
   */
  emitErrors?: boolean;
}

/**
 * Emit a custom event for tygor devtools to display RPC errors.
 * Only emits in browser environment when enabled.
 */
function emitRpcError(service: string, method: string, code: string, message: string, enabled: boolean): void {
  if (!enabled) return;
  if (typeof window !== "undefined" && typeof CustomEvent !== "undefined") {
    window.dispatchEvent(
      new CustomEvent("tygor:rpc-error", {
        detail: { service, method, code, message, timestamp: Date.now() },
      })
    );
  }
}

/**
 * Detect stalled connections due to HTTP/1.1 connection limits.
 * Only warn once per page load in development mode.
 */
let hasWarnedAboutConnectionLimit = false;

function warnIfConnectionStalled(opId: string, getStatus: () => string): void {
  // Only check in development and in browser
  if (typeof import.meta === "undefined" || !(import.meta as any).env?.DEV) return;
  if (typeof window === "undefined") return;

  // Only warn on http://localhost (not https, not production)
  if (window.location.protocol !== "http:" || window.location.hostname !== "localhost") return;

  // Check after 3 seconds if still connecting
  setTimeout(() => {
    if (hasWarnedAboutConnectionLimit) return;
    if (getStatus() !== "connecting") return;

    const message = `"${opId}" has been connecting for 3+ seconds. ` +
      `This is likely caused by browser HTTP/1.1 connection limits (~6 per origin). ` +
      `Enable HTTP/2 by adding @vitejs/plugin-basic-ssl to your vite.config.`;
    const docsUrl = "https://github.com/ahimsalabs/tygor/blob/main/docs/http2.md";

    console.warn(
      "%c🐯 tygor: SSE connection stalled",
      "color: orange; font-weight: bold",
      `\n\n"${opId}" has been connecting for 3+ seconds.\n` +
      `This is likely caused by browser HTTP/1.1 connection limits (~6 per origin).\n\n` +
      `Enable HTTP/2 for unlimited connections:\n` +
      `  1. Install: npm install -D @vitejs/plugin-basic-ssl\n` +
      `  2. Add to vite.config.js:\n` +
      `     import basicSsl from "@vitejs/plugin-basic-ssl"\n` +
      `     plugins: [basicSsl(), ...]\n` +
      `  3. Restart dev server\n\n` +
      `See: ${docsUrl}`
    );

    // Emit event for tygor devtools
    if (typeof CustomEvent !== "undefined") {
      window.dispatchEvent(new CustomEvent("tygor:connection-stalled", {
        detail: { opId, message, docsUrl, timestamp: Date.now() },
      }));
    }

    hasWarnedAboutConnectionLimit = true;
  }, 3000);
}

export interface ServiceRegistry<Manifest extends Record<string, any>> {
  manifest: Manifest;
  metadata: Record<string, { path: string; primitive: "query" | "exec" | "stream" | "livevalue" }>;
}

/**
 * Options for streaming requests.
 */
export interface StreamOptions {
  /** AbortSignal to cancel the stream */
  signal?: AbortSignal;
}

/**
 * Stream represents a server-sent event stream.
 * It provides subscription to the latest value plus AsyncIterable for consuming all events.
 *
 * @example
 * // React - subscribe to latest value (same as LiveValue)
 * const result = useSyncExternalStore(
 *   stream.subscribe,
 *   stream.getSnapshot,
 *   stream.getSnapshot
 * );
 *
 * // For await - consume each event
 * for await (const event of stream) {
 *   console.log(event);
 * }
 */
export interface Stream<T> extends AsyncIterable<T> {
  /** Subscribe to state changes. Returns unsubscribe function. */
  subscribe(listener: (result: SubscriptionResult<T>) => void): () => void;
  /** Get the current snapshot synchronously. */
  getSnapshot(): SubscriptionResult<T>;
}

/**
 * SubscriptionStatus represents the connection state of a subscription.
 * - connecting: Initial connection attempt
 * - connected: Successfully connected and receiving data
 * - reconnecting: Connection lost, attempting to reconnect automatically
 * - completed: Stream ended normally (streams only, won't reconnect)
 * - error: Connection failed with an error
 * - disconnected: Intentionally disconnected (no more subscribers)
 */
export type SubscriptionStatus = "connecting" | "connected" | "reconnecting" | "completed" | "error" | "disconnected";

const MAX_RECONNECT_ATTEMPTS = 3;

function isRetryableStreamStatus(status: number): boolean {
  return status === 502 || status === 503 || status === 504;
}

function sseFieldValue(line: string, field: string): string | undefined {
  if (!line.startsWith(`${field}:`)) return undefined;
  const value = line.slice(field.length + 1);
  return value.startsWith(" ") ? value.slice(1) : value;
}

/**
 * SubscriptionResult is the combined state of a subscription (LiveValue or Stream).
 * Follows a similar pattern to TanStack Query's QueryObserverResult.
 */
export interface SubscriptionResult<T> {
  /** The current data value, if available */
  data: T | undefined;
  /** Connection status */
  status: SubscriptionStatus;
  /** Error if status is 'error' */
  error: Error | undefined;
  /** Timestamp when status last changed */
  statusUpdatedAt: number;
  /** Timestamp when data was last updated */
  dataUpdatedAt: number | undefined;
  /** Convenience: true if status === 'connecting' */
  isConnecting: boolean;
  /** Convenience: true if status === 'connected' */
  isConnected: boolean;
  /** Convenience: true if status === 'reconnecting' */
  isReconnecting: boolean;
  /** Convenience: true if status === 'completed' */
  isCompleted: boolean;
  /** Convenience: true if status === 'error' */
  isError: boolean;
  /** Convenience: true if status === 'disconnected' */
  isDisconnected: boolean;
}

/**
 * LiveValue represents a server-side state that syncs to clients in real-time.
 *
 * The subscribe method follows the external store contract expected by
 * React's useSyncExternalStore and similar patterns in other frameworks.
 * Terminal errors stop reconnecting while the LiveValue is observed. Removing
 * its last subscriber ends that connection session and changes the status to
 * disconnected; a later subscription starts a new session while retaining the
 * last received data.
 *
 * @example
 * // Vanilla JS
 * const unsubscribe = client.Message.State.subscribe((result) => {
 *   console.log(result.status, result.data);
 * });
 *
 * // React with useSyncExternalStore
 * const result = useSyncExternalStore(
 *   liveValue.subscribe,
 *   liveValue.getSnapshot,
 *   liveValue.getSnapshot
 * );
 *
 * // Or use a selector for granular updates
 * const data = useSyncExternalStore(
 *   liveValue.subscribe,
 *   () => liveValue.getSnapshot().data,
 *   () => liveValue.getSnapshot().data
 * );
 */
export interface LiveValue<T> {
  /** Subscribe to all livevalue state changes. Returns unsubscribe function. */
  subscribe(listener: (result: SubscriptionResult<T>) => void): () => void;
  /** Get the current snapshot synchronously. */
  getSnapshot(): SubscriptionResult<T>;
}

export function createClient<Manifest extends Record<string, any>>(
  registry: ServiceRegistry<Manifest>,
  config: ClientConfig = {}
): Client<Manifest> {
  const fetchFn = config.fetch || globalThis.fetch;

  // Determine validation settings
  const schemas = config.schemas;
  const validateRequest = schemas && (config.validate?.request ?? true); // Default: true if schemas provided
  const validateResponse = schemas && (config.validate?.response ?? false); // Default: false

  // Whether to emit RPC errors to devtools (default: true)
  const emitErrors = config.emitErrors ?? true;

  // Cache livevalue clients so multiple accesses return the same instance
  const liveValueCache = new Map<string, LiveValue<unknown>>();

  return new Proxy(
    {},
    {
      get: (_target, service: string) => {
        return new Proxy(
          {},
          {
            get: (_target, method: string) => {
              const opId = `${service}.${method}`;
              const meta = registry.metadata[opId];
              if (!meta) {
                throw new Error(`Unknown service method: ${opId}`);
              }

              // Streaming endpoints return Stream (AsyncIterable + subscribe)
              if (meta.primitive === "stream") {
                return (req: any = {}, options?: StreamOptions) => {
                  return createSSEStream(
                    opId,
                    service,
                    method,
                    meta,
                    req,
                    options,
                    config,
                    fetchFn,
                    schemas,
                    validateRequest,
                    validateResponse,
                    emitErrors
                  );
                };
              }

              // LiveValue endpoints return cached LiveValue (same instance on every access)
              if (meta.primitive === "livevalue") {
                let liveValue = liveValueCache.get(opId);
                if (!liveValue) {
                  liveValue = createLiveValueClient(
                    opId,
                    service,
                    method,
                    meta,
                    config,
                    fetchFn,
                    schemas,
                    validateRequest,
                    validateResponse,
                    emitErrors
                  );
                  liveValueCache.set(opId, liveValue);
                }
                return liveValue;
              }

              // Unary endpoints return Promise
              return async (req: any = {}) => {
                // Request validation (before sending)
                if (validateRequest && schemas?.[opId]?.request) {
                  const schema = schemas[opId].request;
                  const result = await schema["~standard"].validate(req);
                  if (result.issues) {
                    const err = new ValidationError(opId, "request", result.issues);
                    emitRpcError(service, method, "validation_error", err.message, emitErrors);
                    throw err;
                  }
                }

                const headers = config.headers ? config.headers() : {};
                let url = (config.baseUrl || "") + meta.path;
                const httpMethod = meta.primitive === "query" ? "GET" : "POST";
                const options: RequestInit = {
                  method: httpMethod,
                  headers: {
                    ...headers,
                  },
                };

                // Query primitive uses query parameters (no body)
                const usesQueryParams = meta.primitive === "query";

                if (usesQueryParams) {
                  const params = new URLSearchParams();
                  // Sort keys for consistent URL generation (important for caching)
                  const sortedKeys = Object.keys(req || {}).sort();
                  sortedKeys.forEach((key) => {
                    const value = req[key];
                    if (Array.isArray(value)) {
                      value.forEach((v) => params.append(key, String(v)));
                    } else if (value !== undefined && value !== null) {
                      params.append(key, String(value));
                    }
                  });
                  const qs = params.toString();
                  if (qs) {
                    url += "?" + qs;
                  }
                } else {
                  options.headers = {
                    ...options.headers,
                    "Content-Type": "application/json",
                  };
                  options.body = JSON.stringify(req);
                }

                let res: globalThis.Response;
                try {
                  res = await fetchFn(url, options);
                } catch (e) {
                  // Network error (server down, CORS, DNS failure, etc.)
                  const msg = e instanceof Error ? e.message : "Network error";
                  emitRpcError(service, method, "network_error", msg, emitErrors);
                  throw new TransportError(msg, 0, e);
                }
                const httpStatus = res.status;

                // Try to parse as JSON
                let rawBody = "";
                let envelope: Response<any>;
                try {
                  // Clone response so we can read body twice if needed
                  rawBody = await res.clone().text();
                  envelope = JSON.parse(rawBody);
                } catch {
                  // JSON parse failed - this is a transport error (proxy HTML page, etc.)
                  const msg = res.statusText || "Failed to parse response";
                  emitRpcError(service, method, "transport_error", msg, emitErrors);
                  throw new TransportError(msg, httpStatus, undefined, rawBody.slice(0, 1000));
                }

                // Handle malformed or null envelope
                if (!envelope || typeof envelope !== "object") {
                  emitRpcError(service, method, "transport_error", "Invalid response format", emitErrors);
                  throw new TransportError("Invalid response format", httpStatus, undefined, rawBody.slice(0, 1000));
                }

                // Validate envelope has expected structure
                if (!("result" in envelope) && !("error" in envelope)) {
                  const msg = "Invalid response format: missing result or error field";
                  emitRpcError(service, method, "transport_error", msg, emitErrors);
                  throw new TransportError(msg, httpStatus, undefined, rawBody.slice(0, 1000));
                }

                // Check for error in envelope - this is an application-level error
                if (envelope.error) {
                  const code = (envelope.error.code || "internal") as ErrorCode;
                  const msg = envelope.error.message || "Unknown error";
                  emitRpcError(service, method, code, msg, emitErrors);
                  throw new ServerError(code, msg, httpStatus, envelope.error.details);
                }

                // Response validation (after receiving)
                if (validateResponse && schemas?.[opId]?.response) {
                  const schema = schemas[opId].response;
                  const result = await schema["~standard"].validate(envelope.result);
                  if (result.issues) {
                    const err = new ValidationError(opId, "response", result.issues);
                    emitRpcError(service, method, "validation_error", err.message, emitErrors);
                    throw err;
                  }
                }

                // Return the unwrapped result
                return envelope.result;
              };
            },
          }
        );
      },
    }
  ) as Client<Manifest>;
}

/**
 * Creates a Stream that emits SSE events from the server.
 * Supports both subscribe/getSnapshot (for reactive frameworks) and AsyncIterable (for await).
 */
function createSSEStream<T>(
  opId: string,
  service: string,
  method: string,
  meta: { path: string; primitive: "query" | "exec" | "stream" | "livevalue" },
  req: any,
  options: StreamOptions | undefined,
  config: ClientConfig,
  fetchFn: FetchFunction,
  schemas: SchemaMap | undefined,
  validateRequest: boolean | undefined,
  validateResponse: boolean | undefined,
  emitErrors: boolean
): Stream<T> {
  // Combined state (same pattern as LiveValue)
  let currentData: T | undefined = undefined;
  let currentStatus: SubscriptionStatus = "disconnected";
  let currentError: Error | undefined = undefined;
  let statusUpdatedAt = Date.now();
  let dataUpdatedAt: number | undefined = undefined;
  let currentSnapshot = makeSubscriptionResult<T>(
    currentStatus,
    currentData,
    currentError,
    statusUpdatedAt,
    dataUpdatedAt,
  );

  // Listeners get notified on any state change
  const listeners = new Set<(result: SubscriptionResult<T>) => void>();
  // Separate data listeners for AsyncIterator
  const dataListeners = new Set<(value: T) => void>();

  const getSnapshot = (): SubscriptionResult<T> => currentSnapshot;

  const notify = () => {
    currentSnapshot = makeSubscriptionResult(
      currentStatus,
      currentData,
      currentError,
      statusUpdatedAt,
      dataUpdatedAt,
    );
    listeners.forEach((listener) => listener(currentSnapshot));
  };

  const setStatus = (status: SubscriptionStatus, error?: Error) => {
    currentStatus = status;
    currentError = error;
    statusUpdatedAt = Date.now();
    notify();
    if (status === "completed" || status === "error") detachUserAbortListener();
  };

  const setData = (data: T) => {
    currentData = data;
    dataUpdatedAt = Date.now();
    // Notify data listeners for AsyncIterator
    dataListeners.forEach((listener) => listener(data));
    notify();
  };

  // Connection state
  let controller: AbortController | null = null;
  let connectionPromise: Promise<void> | null = null;
  let reconnectAttempt = 0;
  let reconnectTimeout: ReturnType<typeof setTimeout> | null = null;
  let lastEventId = "";
  // Track if we were intentionally aborted (user-provided signal)
  let userAborted = options?.signal?.aborted ?? false;
  let userAbortListenerAttached = false;
  // Track if we've ever successfully connected (for connecting vs reconnecting)
  let hasConnected = false;
  const hasConsumers = () => listeners.size > 0 || dataListeners.size > 0;

  const scheduleReconnect = (error: Error) => {
    // Don't reconnect if user aborted or no listeners
    if (userAborted || reconnectTimeout || !hasConsumers()) return;

    if (reconnectAttempt >= MAX_RECONNECT_ATTEMPTS) {
      setStatus("error", error);
      return;
    }

    reconnectAttempt++;
    // Exponential backoff: 100ms, 200ms, 400ms, 800ms, 1600ms, max 3000ms
    const delay = Math.min(100 * Math.pow(2, reconnectAttempt - 1), 3000);

    reconnectTimeout = setTimeout(() => {
      reconnectTimeout = null;
      if (hasConsumers()) {
        connect();
      }
    }, delay);
  };

  const connect = () => {
    if (userAborted || currentStatus === "completed" || currentStatus === "error" || !hasConsumers()) {
      return Promise.resolve();
    }
    if (connectionPromise) {
      // If controller was aborted, wait for cleanup then create new connection
      if (controller?.signal.aborted) {
        return connectionPromise.catch(() => {}).finally(() => connect());
      }
      return connectionPromise;
    }

    const myController = controller = new AbortController();

    setStatus(hasConnected || reconnectAttempt > 0 ? "reconnecting" : "connecting");
    warnIfConnectionStalled(opId, () => currentStatus);

    const myPromise = connectionPromise = (async () => {
      // Request validation (before sending)
      if (validateRequest && schemas?.[opId]?.request) {
        try {
          const schema = schemas[opId].request;
          const result = await schema["~standard"].validate(req);
          if (myController.signal.aborted) return;
          if (result.issues) {
            const err = new ValidationError(opId, "request", result.issues);
            emitRpcError(service, method, "validation_error", err.message, emitErrors);
            setStatus("error", err);
            return;
          }
        } catch (e) {
          if (myController.signal.aborted) return;
          const err = e instanceof Error ? e : new Error("Request validation failed");
          emitRpcError(service, method, "validation_error", err.message, emitErrors);
          setStatus("error", err);
          return;
        }
      }

      let url: string;
      let fetchOptions: RequestInit;
      try {
        const headers = config.headers ? config.headers() : {};
        url = (config.baseUrl || "") + meta.path;
        fetchOptions = {
          method: "POST",
          headers: {
            ...headers,
            "Content-Type": "application/json",
            Accept: "text/event-stream",
            ...(lastEventId ? { "Last-Event-ID": lastEventId } : {}),
          },
          body: JSON.stringify(req),
          signal: myController.signal,
        };
      } catch (e) {
        if (myController.signal.aborted) return;
        const err = e instanceof Error ? e : new Error("Failed to prepare stream request");
        setStatus("error", err);
        return;
      }

      let res: globalThis.Response;
      try {
        res = await fetchFn(url, fetchOptions);
      } catch (e) {
        if (myController.signal.aborted) return;
        const msg = e instanceof Error ? e.message : "Network error";
        emitRpcError(service, method, "network_error", msg, emitErrors);
        setStatus("reconnecting");
        scheduleReconnect(new TransportError(msg, 0, e));
        return;
      }
      if (myController.signal.aborted) return;

      const httpStatus = res.status;

      // Check for non-SSE error response
      const contentType = res.headers.get("Content-Type") || "";
      if (!contentType.includes("text/event-stream")) {
        if (isRetryableStreamStatus(httpStatus)) {
          const err = new TransportError(res.statusText || `HTTP ${httpStatus}`, httpStatus);
          emitRpcError(service, method, "transport_error", err.message, emitErrors);
          setStatus("reconnecting");
          scheduleReconnect(err);
          return;
        }
        let rawBody = "";
        try {
          rawBody = await res.text();
          if (myController.signal.aborted) return;
          const envelope = JSON.parse(rawBody);
          if (envelope.error) {
            const code = (envelope.error.code || "internal") as ErrorCode;
            const msg = envelope.error.message || "Unknown error";
            emitRpcError(service, method, code, msg, emitErrors);
            setStatus("error", new ServerError(code, msg, httpStatus, envelope.error.details));
            return;
          }
        } catch (e) {
          if (myController.signal.aborted) return;
          if (e instanceof ServerError) {
            setStatus("error", e);
            return;
          }
          const msg = res.statusText || "Failed to establish stream";
          emitRpcError(service, method, "transport_error", msg, emitErrors);
          setStatus("error", new TransportError(msg, httpStatus, undefined, rawBody.slice(0, 1000)));
          return;
        }
      }

      if (!res.body) {
        setStatus("error", new TransportError("Response body is null", httpStatus));
        return;
      }

      setStatus("connected");
      hasConnected = true;
      reconnectAttempt = 0; // Reset on successful connection


      const reader = res.body.getReader();
      const decoder = new TextDecoder();
      let buffer = "";

      try {
        while (true) {
          const { value, done } = await reader.read();
          if (myController.signal.aborted) return;
          if (done) break;

          buffer += decoder.decode(value, { stream: true });

          // Parse complete SSE events from buffer
          let eventEnd: number;
          while ((eventEnd = buffer.indexOf("\n\n")) !== -1) {
            const eventText = buffer.slice(0, eventEnd);
            buffer = buffer.slice(eventEnd + 2);

            // Parse SSE event
            const lines = eventText.split("\n");
            for (const line of lines) {
              const id = sseFieldValue(line, "id");
              if (id !== undefined && !id.includes("\0")) lastEventId = id;
            }
            for (const line of lines) {
              if (line.startsWith("data: ")) {
                if (myController.signal.aborted) return;
                const data = line.slice(6);
                try {
                  const envelope = JSON.parse(data) as Response<T>;

                  if (envelope.error) {
                    const code = (envelope.error.code || "internal") as ErrorCode;
                    const msg = envelope.error.message || "Unknown error";
                    emitRpcError(service, method, code, msg, emitErrors);
                    setStatus("error", new ServerError(code, msg, httpStatus, envelope.error.details));
                    return;
                  }

                  // Response validation
                  if (validateResponse && schemas?.[opId]?.response) {
                    const schema = schemas[opId].response;
                    const result = await schema["~standard"].validate(envelope.result);
                    if (myController.signal.aborted) return;
                    if (result.issues) {
                      const err = new ValidationError(opId, "response", result.issues);
                      emitRpcError(service, method, "validation_error", err.message, emitErrors);
                      setStatus("error", err);
                      return;
                    }
                  }

                  // Update data (notifies both listeners and dataListeners)
                  if (myController.signal.aborted) return;
                  setData(envelope.result as T);
                } catch (e) {
                  if (myController.signal.aborted) return;
                  if (e instanceof ServerError || e instanceof ValidationError) {
                    setStatus("error", e);
                    return;
                  }
                  emitRpcError(service, method, "transport_error", "Failed to parse SSE event", emitErrors);
                  setStatus("error", new TransportError("Failed to parse SSE event", httpStatus, e, data));
                  return;
                }
              }
            }
          }
        }
      } catch (e) {
        // Rethrow to be caught by outer catch
        throw e;
      } finally {
        try {
          await reader.cancel();
        } catch {
          // Ignore errors from cancel
        }
        reader.releaseLock();
      }

      // Stream ended cleanly - this is intentional completion, don't reconnect
      if (myController.signal.aborted) return;
      setStatus("completed");
    })().catch((err) => {
      // Silently ignore AbortError from cleanup (intentional disconnect)
      if (myController.signal.aborted) return;
      // Network/body interruption - keep consumers active while reconnecting.
      const msg = err instanceof Error ? err.message : "Stream connection interrupted";
      emitRpcError(service, method, "network_error", msg, emitErrors);
      setStatus("reconnecting");
      scheduleReconnect(new TransportError(msg, 0, err));
    }).finally(() => {
      // Only clear if we're still the active connection (prevent race with new connections)
      if (connectionPromise === myPromise) {
        connectionPromise = null;
      }
      if (controller === myController) {
        controller = null;
      }
    });

    return connectionPromise;
  };

  function disconnect() {
    // Cancel any pending reconnect
    if (reconnectTimeout) {
      clearTimeout(reconnectTimeout);
      reconnectTimeout = null;
    }
    reconnectAttempt = 0;
    if (controller) {
      controller.abort();
      // Don't set controller or connectionPromise to null here
      // Let the finally block handle cleanup after abort completes
    }
    if (currentStatus !== "completed" && currentStatus !== "error" && currentStatus !== "disconnected") {
      setStatus("disconnected");
    }
  }

  function abortFromUser() {
    userAborted = true;
    detachUserAbortListener();
    const notifyCancellation = currentStatus === "disconnected" && hasConsumers();
    disconnect();
    if (notifyCancellation) setStatus("disconnected");
  }

  function attachUserAbortListener() {
    const signal = options?.signal;
    if (!signal || userAborted || userAbortListenerAttached) return;
    if (signal.aborted) {
      userAborted = true;
      return;
    }
    signal.addEventListener("abort", abortFromUser, { once: true });
    userAbortListenerAttached = true;
    // An abort between the check and listener registration does not dispatch
    // the event to this newly registered listener.
    if (signal.aborted) abortFromUser();
  }

  function detachUserAbortListener() {
    if (!userAbortListenerAttached) return;
    options!.signal!.removeEventListener("abort", abortFromUser);
    userAbortListenerAttached = false;
  }

  // Create async iterator for for-await usage
  const createAsyncIterator = (): AsyncIterator<T> => {
    type Waiter = {
      resolve: (result: IteratorResult<T>) => void;
      reject: (error: Error) => void;
    };
    type IteratorState = "active" | "completed" | "cancelled" | "failed";

    const values: T[] = [];
    const waiters: Waiter[] = [];
    let state: IteratorState = "active";
    let terminalError: Error | undefined;
    let listening = false;

    const cleanup = () => {
      if (!listening) return;
      listening = false;
      dataListeners.delete(onData);
      listeners.delete(onStatus);
      if (dataListeners.size === 0 && listeners.size === 0) {
        disconnect();
        detachUserAbortListener();
      }
    };

    const finish = (nextState: Exclude<IteratorState, "active">, error?: Error) => {
      if (state !== "active") return;
      state = nextState;
      terminalError = error;
      if (nextState === "failed" || nextState === "cancelled") values.length = 0;
      cleanup();

      for (const waiter of waiters.splice(0)) {
        if (nextState === "failed") {
          waiter.reject(error!);
        } else {
          waiter.resolve({ done: true, value: undefined as any });
        }
      }
    };

    const onData = (value: T) => {
      if (state !== "active") return;
      const waiter = waiters.shift();
      if (waiter) {
        waiter.resolve({ done: false, value });
      } else {
        values.push(value);
      }
    };

    // Also listen for errors/completion via status changes
    const onStatus = (result: SubscriptionResult<T>) => {
      if (result.status === "completed") {
        finish("completed");
      } else if (result.status === "error") {
        finish("failed", result.error ?? new Error("Stream failed"));
      } else if (result.status === "disconnected") {
        finish("cancelled");
      }
    };

    if (currentStatus === "completed") {
      state = "completed";
    } else if (currentStatus === "error") {
      state = "failed";
      terminalError = currentError ?? new Error("Stream failed");
    } else if (userAborted) {
      state = "cancelled";
    } else {
      attachUserAbortListener();
      if (userAborted) {
        state = "cancelled";
      } else {
        const hadConsumers = listeners.size > 0 || dataListeners.size > 0;
        dataListeners.add(onData);
        listeners.add(onStatus);
        listening = true;
        if (!hadConsumers) connect();
      }
    }

    if (listening && currentStatus === "disconnected") {
      connect();
    }

    return {
      async next(): Promise<IteratorResult<T>> {
        if (state === "failed") {
          throw terminalError;
        }
        if (values.length > 0) {
          return { done: false, value: values.shift()! };
        }
        if (state !== "active") {
          return { done: true, value: undefined as any };
        }
        return new Promise((resolve, reject) => {
          waiters.push({ resolve, reject });
        });
      },
      async return(): Promise<IteratorResult<T>> {
        if (state === "completed") {
          state = "cancelled";
          values.length = 0;
        } else {
          finish("cancelled");
        }
        return { done: true, value: undefined as any };
      },
    };
  };

  return {
    [Symbol.asyncIterator](): AsyncIterator<T> {
      return createAsyncIterator();
    },

    subscribe(listener: (result: SubscriptionResult<T>) => void): () => void {
      if (currentStatus !== "completed" && currentStatus !== "error") {
        attachUserAbortListener();
      }
      listeners.add(listener);

      // Start connection if this is the first subscriber
      if (listeners.size === 1 && dataListeners.size === 0) {
        connect();
      }

      // Immediately emit current state
      listener(getSnapshot());

      return () => {
        listeners.delete(listener);
        if (listeners.size === 0 && dataListeners.size === 0) {
          disconnect();
          detachUserAbortListener();
        }
      };
    },

    getSnapshot,
  };
}

/**
 * Helper to create a SubscriptionResult from current state.
 */
function makeSubscriptionResult<T>(
  status: SubscriptionStatus,
  data: T | undefined,
  error: Error | undefined,
  statusUpdatedAt: number,
  dataUpdatedAt: number | undefined
): SubscriptionResult<T> {
  return {
    data,
    status,
    error,
    statusUpdatedAt,
    dataUpdatedAt,
    isConnecting: status === "connecting",
    isConnected: status === "connected",
    isReconnecting: status === "reconnecting",
    isCompleted: status === "completed",
    isError: status === "error",
    isDisconnected: status === "disconnected",
  };
}

/**
 * Creates a LiveValue client for synchronized state subscriptions.
 * Unlike streams, livevalues represent current state - subscribers get the
 * current value immediately, then updates as they occur.
 *
 * Follows the external store contract (subscribe + getSnapshot) for
 * compatibility with React's useSyncExternalStore and similar patterns.
 */
function createLiveValueClient<T>(
  opId: string,
  service: string,
  method: string,
  meta: { path: string; primitive: "query" | "exec" | "stream" | "livevalue" },
  config: ClientConfig,
  fetchFn: FetchFunction,
  schemas: SchemaMap | undefined,
  validateRequest: boolean | undefined,
  validateResponse: boolean | undefined,
  emitErrors: boolean
): LiveValue<T> {
  // Combined state
  let currentData: T | undefined = undefined;
  let currentStatus: SubscriptionStatus = "disconnected";
  let currentError: Error | undefined = undefined;
  let statusUpdatedAt = Date.now();
  let dataUpdatedAt: number | undefined = undefined;
  let currentSnapshot = makeSubscriptionResult<T>(
    currentStatus,
    currentData,
    currentError,
    statusUpdatedAt,
    dataUpdatedAt,
  );

  // Listeners get notified on any state change
  const listeners = new Set<(result: SubscriptionResult<T>) => void>();

  const getSnapshot = (): SubscriptionResult<T> => currentSnapshot;

  const notify = () => {
    currentSnapshot = makeSubscriptionResult(
      currentStatus,
      currentData,
      currentError,
      statusUpdatedAt,
      dataUpdatedAt,
    );
    listeners.forEach((listener) => listener(currentSnapshot));
  };

  const setStatus = (status: SubscriptionStatus, error?: Error) => {
    currentStatus = status;
    currentError = error;
    statusUpdatedAt = Date.now();
    notify();
  };

  const setData = (data: T) => {
    currentData = data;
    dataUpdatedAt = Date.now();
    notify();
  };

  // Single shared connection
  let controller: AbortController | null = null;
  let connectionPromise: Promise<void> | null = null;
  let reconnectAttempt = 0;
  let reconnectTimeout: ReturnType<typeof setTimeout> | null = null;
  let lastEventId = "";
  // Track if we've ever successfully connected (for connecting vs reconnecting)
  let hasConnected = false;
  const hasConsumers = () => listeners.size > 0;

  const scheduleReconnect = (error: Error) => {
    // Only reconnect if we still have listeners
    if (reconnectTimeout || !hasConsumers()) return;

    if (reconnectAttempt >= MAX_RECONNECT_ATTEMPTS) {
      setStatus("error", error);
      return;
    }

    reconnectAttempt++;
    // Exponential backoff: 100ms, 200ms, 400ms, 800ms, 1600ms, max 3000ms
    const delay = Math.min(100 * Math.pow(2, reconnectAttempt - 1), 3000);

    reconnectTimeout = setTimeout(() => {
      reconnectTimeout = null;
      if (hasConsumers()) {
        connect();
      }
    }, delay);
  };

  const connect = () => {
    if (currentStatus === "error" || !hasConsumers()) {
      return Promise.resolve();
    }
    if (connectionPromise) {
      // If controller was aborted, wait for cleanup then create new connection
      if (controller?.signal.aborted) {
        return connectionPromise.catch(() => {}).finally(() => connect());
      }
      return connectionPromise;
    }

    const myController = controller = new AbortController();
    setStatus(hasConnected || reconnectAttempt > 0 ? "reconnecting" : "connecting");
    warnIfConnectionStalled(opId, () => currentStatus);

    const myPromise = connectionPromise = (async () => {
      const req = {};

      let url: string;
      let fetchOptions: RequestInit;
      try {
        const headers = config.headers ? config.headers() : {};
        url = (config.baseUrl || "") + meta.path;
        fetchOptions = {
          method: "POST",
          headers: {
            ...headers,
            "Content-Type": "application/json",
            Accept: "text/event-stream",
            ...(lastEventId ? { "Last-Event-ID": lastEventId } : {}),
          },
          body: JSON.stringify(req),
          signal: myController.signal,
        };
      } catch (e) {
        if (myController.signal.aborted) return;
        const err = e instanceof Error ? e : new Error("Failed to prepare LiveValue request");
        setStatus("error", err);
        return;
      }

      let res: globalThis.Response;
      try {
        res = await fetchFn(url, fetchOptions);
      } catch (e) {
        if (myController.signal.aborted) return;
        const msg = e instanceof Error ? e.message : "Network error";
        emitRpcError(service, method, "network_error", msg, emitErrors);
        setStatus("reconnecting");
        scheduleReconnect(new TransportError(msg, 0, e));
        return;
      }
      if (myController.signal.aborted) return;

      const httpStatus = res.status;

      // Check for non-SSE error response
      const contentType = res.headers.get("Content-Type") || "";
      if (!contentType.includes("text/event-stream")) {
        if (isRetryableStreamStatus(httpStatus)) {
          const err = new TransportError(res.statusText || `HTTP ${httpStatus}`, httpStatus);
          emitRpcError(service, method, "transport_error", err.message, emitErrors);
          setStatus("reconnecting");
          scheduleReconnect(err);
          return;
        }
        let rawBody = "";
        try {
          rawBody = await res.text();
          if (myController.signal.aborted) return;
          const envelope = JSON.parse(rawBody);
          if (envelope.error) {
            const code = (envelope.error.code || "internal") as ErrorCode;
            const msg = envelope.error.message || "Unknown error";
            emitRpcError(service, method, code, msg, emitErrors);
            setStatus("error", new ServerError(code, msg, httpStatus, envelope.error.details));
            return;
          }
        } catch (e) {
          if (myController.signal.aborted) return;
          if (e instanceof ServerError) {
            setStatus("error", e);
            return;
          }
          const msg = res.statusText || "Failed to establish livevalue subscription";
          emitRpcError(service, method, "transport_error", msg, emitErrors);
          setStatus("error", new TransportError(msg, httpStatus, undefined, rawBody.slice(0, 1000)));
          return;
        }
      }

      if (!res.body) {
        setStatus("error", new TransportError("Response body is null", httpStatus));
        return;
      }

      setStatus("connected");
      hasConnected = true;
      reconnectAttempt = 0; // Reset on successful connection


      const reader = res.body.getReader();
      const decoder = new TextDecoder();
      let buffer = "";

      try {
        while (true) {
          const { value, done } = await reader.read();
          if (myController.signal.aborted) return;
          if (done) break;

          buffer += decoder.decode(value, { stream: true });

          // Parse complete SSE events from buffer
          let eventEnd: number;
          while ((eventEnd = buffer.indexOf("\n\n")) !== -1) {
            const eventText = buffer.slice(0, eventEnd);
            buffer = buffer.slice(eventEnd + 2);

            // Parse SSE event
            const lines = eventText.split("\n");
            for (const line of lines) {
              const id = sseFieldValue(line, "id");
              if (id !== undefined && !id.includes("\0")) lastEventId = id;
            }
            for (const line of lines) {
              if (line.startsWith("data: ")) {
                if (myController.signal.aborted) return;
                const data = line.slice(6);
                try {
                  const envelope = JSON.parse(data) as Response<T>;

                  if (envelope.error) {
                    const code = (envelope.error.code || "internal") as ErrorCode;
                    const msg = envelope.error.message || "Unknown error";
                    emitRpcError(service, method, code, msg, emitErrors);
                    setStatus("error", new ServerError(code, msg, httpStatus, envelope.error.details));
                    return;
                  }

                  // Response validation
                  if (validateResponse && schemas?.[opId]?.response) {
                    const schema = schemas[opId].response;
                    const result = await schema["~standard"].validate(envelope.result);
                    if (myController.signal.aborted) return;
                    if (result.issues) {
                      const err = new ValidationError(opId, "response", result.issues);
                      emitRpcError(service, method, "validation_error", err.message, emitErrors);
                      setStatus("error", err);
                      return;
                    }
                  }

                  // Update data
                  if (myController.signal.aborted) return;
                  setData(envelope.result as T);
                } catch (e) {
                  if (myController.signal.aborted) return;
                  if (e instanceof ServerError || e instanceof ValidationError) {
                    setStatus("error", e);
                    return;
                  }
                  emitRpcError(service, method, "transport_error", "Failed to parse livevalue event", emitErrors);
                  setStatus("error", new TransportError("Failed to parse livevalue event", httpStatus, e, data));
                  return;
                }
              }
            }
          }
        }
      } catch (e) {
        // Rethrow to be caught by outer catch
        throw e;
      } finally {
        try {
          await reader.cancel();
        } catch {
          // Ignore errors from cancel
        }
        reader.releaseLock();
      }

      // LiveValue connection closed unexpectedly - reconnect
      // (LiveValues represent persistent server state, they shouldn't end cleanly)
      if (myController.signal.aborted) return;
      setStatus("reconnecting");
      scheduleReconnect(new TransportError("LiveValue connection ended", httpStatus));
    })().catch((err) => {
      // Silently ignore AbortError from cleanup (intentional disconnect)
      if (myController.signal.aborted) return;
      // Network/body interruption - keep the subscription active while reconnecting.
      const msg = err instanceof Error ? err.message : "LiveValue connection interrupted";
      emitRpcError(service, method, "network_error", msg, emitErrors);
      setStatus("reconnecting");
      scheduleReconnect(new TransportError(msg, 0, err));
    }).finally(() => {
      // Only clear if we're still the active connection (prevent race with new connections)
      if (connectionPromise === myPromise) {
        connectionPromise = null;
      }
      if (controller === myController) {
        controller = null;
      }
    });

    return connectionPromise;
  };

  const disconnect = () => {
    // Cancel any pending reconnect
    if (reconnectTimeout) {
      clearTimeout(reconnectTimeout);
      reconnectTimeout = null;
    }
    reconnectAttempt = 0;
    if (controller) {
      controller.abort();
      // Don't set controller or connectionPromise to null here
      // Let the finally block handle cleanup after abort completes
    }
    if (currentStatus !== "disconnected") setStatus("disconnected");
  };

  return {
    subscribe(listener: (result: SubscriptionResult<T>) => void): () => void {
      listeners.add(listener);

      // Start connection if this is the first subscriber
      if (listeners.size === 1) {
        connect();
      }

      // Immediately emit current state (required by useSyncExternalStore contract)
      listener(getSnapshot());

      return () => {
        listeners.delete(listener);
        // Disconnect if no more subscribers
        if (listeners.size === 0) {
          disconnect();
        }
      };
    },
    getSnapshot,
  };
}

// Type helpers to transform the flat Manifest into a nested Client structure
type ServiceName<K> = K extends `${infer S}.${string}` ? S : never;

// IsOptionalRequest checks if the request parameter can be omitted.
// True when: Record<string, never> (empty), or all fields are optional.
// This allows both client.System.Kill() and client.Tasks.List() to work.
type IsOptionalRequest<T> = {} extends T ? true : false;

type ServiceMethods<M, S extends string> = {
  [K in keyof M as K extends `${S}.${infer Method}` ? Method : never]: M[K] extends {
    req: infer Req;
    res: infer Res;
    primitive: "stream";
  }
    ? IsOptionalRequest<Req> extends true
      ? (req?: Req, options?: StreamOptions) => Stream<Res>
      : (req: Req, options?: StreamOptions) => Stream<Res>
    : M[K] extends {
          res: infer Res;
          primitive: "livevalue";
        }
      ? LiveValue<Res>
      : M[K] extends {
            req: infer Req;
            res: infer Res;
          }
        ? IsOptionalRequest<Req> extends true
          ? (req?: Req) => Promise<Res>
          : (req: Req) => Promise<Res>
        : never;
};

export type Client<M> = {
  [K in keyof M as ServiceName<K>]: ServiceMethods<M, ServiceName<K>>;
};
