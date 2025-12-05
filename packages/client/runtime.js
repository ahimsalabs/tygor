/**
 * TygorError is the base class for all tygor client errors.
 * Use instanceof to narrow to ServerError or TransportError.
 */
export class TygorError extends Error {
    constructor(message, options) {
        super(message, options);
    }
}
/**
 * ServerError represents an application-level error returned by the tygor server.
 * These have a structured code, message, and optional details.
 */
export class ServerError extends TygorError {
    kind = "server";
    code;
    details;
    httpStatus;
    constructor(code, message, httpStatus, details) {
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
    kind = "transport";
    httpStatus;
    rawBody;
    constructor(message, httpStatus, cause, rawBody) {
        super(message, { cause });
        this.name = "TransportError";
        this.httpStatus = httpStatus;
        this.rawBody = rawBody;
    }
}
/**
 * ValidationError is thrown when client-side schema validation fails.
 */
export class ValidationError extends Error {
    kind = "validation";
    issues;
    direction;
    endpoint;
    constructor(endpoint, direction, issues) {
        const paths = issues.map((i) => i.path?.join(".") || "(root)").join(", ");
        super(`${direction} validation failed for ${endpoint}: ${paths}`);
        this.name = "ValidationError";
        this.endpoint = endpoint;
        this.direction = direction;
        this.issues = issues;
    }
}
/**
 * Emit a custom event for tygor devtools to display RPC errors.
 * Only emits in browser environment when enabled.
 */
function emitRpcError(service, method, code, message, enabled) {
    if (!enabled)
        return;
    if (typeof window !== "undefined" && typeof CustomEvent !== "undefined") {
        window.dispatchEvent(new CustomEvent("tygor:rpc-error", {
            detail: { service, method, code, message, timestamp: Date.now() },
        }));
    }
}
export function createClient(registry, config = {}) {
    const fetchFn = config.fetch || globalThis.fetch;
    // Determine validation settings
    const schemas = config.schemas;
    const validateRequest = schemas && (config.validate?.request ?? true); // Default: true if schemas provided
    const validateResponse = schemas && (config.validate?.response ?? false); // Default: false
    // Whether to emit RPC errors to devtools (default: true)
    const emitErrors = config.emitErrors ?? true;
    // Cache atom clients so multiple accesses return the same instance
    const atomCache = new Map();
    return new Proxy({}, {
        get: (_target, service) => {
            return new Proxy({}, {
                get: (_target, method) => {
                    const opId = `${service}.${method}`;
                    const meta = registry.metadata[opId];
                    if (!meta) {
                        throw new Error(`Unknown service method: ${opId}`);
                    }
                    // Streaming endpoints return Stream (AsyncIterable + subscribe)
                    if (meta.primitive === "stream") {
                        return (req = {}, options) => {
                            return createSSEStream(opId, service, method, meta, req, options, config, fetchFn, schemas, validateRequest, validateResponse, emitErrors);
                        };
                    }
                    // Atom endpoints return cached Atom (same instance on every access)
                    if (meta.primitive === "atom") {
                        let atom = atomCache.get(opId);
                        if (!atom) {
                            atom = createAtomClient(opId, service, method, meta, config, fetchFn, schemas, validateRequest, validateResponse, emitErrors);
                            atomCache.set(opId, atom);
                        }
                        return atom;
                    }
                    // Unary endpoints return Promise
                    return async (req = {}) => {
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
                        const options = {
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
                                }
                                else if (value !== undefined && value !== null) {
                                    params.append(key, String(value));
                                }
                            });
                            const qs = params.toString();
                            if (qs) {
                                url += "?" + qs;
                            }
                        }
                        else {
                            options.headers = {
                                ...options.headers,
                                "Content-Type": "application/json",
                            };
                            options.body = JSON.stringify(req);
                        }
                        let res;
                        try {
                            res = await fetchFn(url, options);
                        }
                        catch (e) {
                            // Network error (server down, CORS, DNS failure, etc.)
                            const msg = e instanceof Error ? e.message : "Network error";
                            emitRpcError(service, method, "network_error", msg, emitErrors);
                            throw new TransportError(msg, 0, e);
                        }
                        const httpStatus = res.status;
                        // Try to parse as JSON
                        let rawBody = "";
                        let envelope;
                        try {
                            // Clone response so we can read body twice if needed
                            rawBody = await res.clone().text();
                            envelope = JSON.parse(rawBody);
                        }
                        catch {
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
                            const code = (envelope.error.code || "internal");
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
            });
        },
    });
}
/**
 * Creates a Stream that emits SSE events from the server.
 * Supports both subscribe/getSnapshot (for reactive frameworks) and AsyncIterable (for await).
 */
function createSSEStream(opId, service, method, meta, req, options, config, fetchFn, schemas, validateRequest, validateResponse, emitErrors) {
    // Combined state (same pattern as Atom)
    let currentData = undefined;
    let currentStatus = "disconnected";
    let currentError = undefined;
    let statusUpdatedAt = Date.now();
    let dataUpdatedAt = undefined;
    // Listeners get notified on any state change
    const listeners = new Set();
    // Separate data listeners for AsyncIterator
    const dataListeners = new Set();
    const getSnapshot = () => {
        return makeSubscriptionResult(currentStatus, currentData, currentError, statusUpdatedAt, dataUpdatedAt);
    };
    const notify = () => {
        const result = getSnapshot();
        listeners.forEach((listener) => listener(result));
    };
    const setStatus = (status, error) => {
        currentStatus = status;
        currentError = error;
        statusUpdatedAt = Date.now();
        notify();
    };
    const setData = (data) => {
        currentData = data;
        dataUpdatedAt = Date.now();
        // Notify data listeners for AsyncIterator
        dataListeners.forEach((listener) => listener(data));
        notify();
    };
    // Connection state
    let controller = null;
    let connectionPromise = null;
    let reconnectAttempt = 0;
    let reconnectTimeout = null;
    // Track if we were intentionally aborted (user-provided signal)
    let userAborted = false;
    // Track if we've ever successfully connected (for connecting vs reconnecting)
    let hasConnected = false;
    const scheduleReconnect = () => {
        // Don't reconnect if user aborted or no listeners
        if (userAborted || (listeners.size === 0 && dataListeners.size === 0))
            return;
        reconnectAttempt++;
        // Exponential backoff: 100ms, 200ms, 400ms, 800ms, 1600ms, max 3000ms
        const delay = Math.min(100 * Math.pow(2, reconnectAttempt - 1), 3000);
        reconnectTimeout = setTimeout(() => {
            reconnectTimeout = null;
            if (listeners.size > 0 || dataListeners.size > 0) {
                connect();
            }
        }, delay);
    };
    const connect = () => {
        if (connectionPromise)
            return connectionPromise;
        controller = new AbortController();
        // Combine with options signal if provided
        if (options?.signal) {
            if (options.signal.aborted) {
                userAborted = true;
                setStatus("disconnected");
                return Promise.resolve();
            }
            options.signal.addEventListener("abort", () => {
                userAborted = true;
                controller?.abort();
            });
        }
        setStatus(hasConnected ? "reconnecting" : "connecting");
        connectionPromise = (async () => {
            // Request validation (before sending)
            if (validateRequest && schemas?.[opId]?.request) {
                const schema = schemas[opId].request;
                const result = await schema["~standard"].validate(req);
                if (result.issues) {
                    const err = new ValidationError(opId, "request", result.issues);
                    emitRpcError(service, method, "validation_error", err.message, emitErrors);
                    setStatus("error", err);
                    return;
                }
            }
            const headers = config.headers ? config.headers() : {};
            const url = (config.baseUrl || "") + meta.path;
            const fetchOptions = {
                method: "POST",
                headers: {
                    ...headers,
                    "Content-Type": "application/json",
                    Accept: "text/event-stream",
                },
                body: JSON.stringify(req),
                signal: controller.signal,
            };
            let res;
            try {
                res = await fetchFn(url, fetchOptions);
            }
            catch (e) {
                if (e.name === "AbortError") {
                    setStatus("disconnected");
                    return;
                }
                const msg = e instanceof Error ? e.message : "Network error";
                emitRpcError(service, method, "network_error", msg, emitErrors);
                setStatus("error", new TransportError(msg, 0, e));
                return;
            }
            const httpStatus = res.status;
            // Check for non-SSE error response
            const contentType = res.headers.get("Content-Type") || "";
            if (!contentType.includes("text/event-stream")) {
                let rawBody = "";
                try {
                    rawBody = await res.text();
                    const envelope = JSON.parse(rawBody);
                    if (envelope.error) {
                        const code = (envelope.error.code || "internal");
                        const msg = envelope.error.message || "Unknown error";
                        emitRpcError(service, method, code, msg, emitErrors);
                        setStatus("error", new ServerError(code, msg, httpStatus, envelope.error.details));
                        return;
                    }
                }
                catch (e) {
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
                    if (done)
                        break;
                    buffer += decoder.decode(value, { stream: true });
                    // Parse complete SSE events from buffer
                    let eventEnd;
                    while ((eventEnd = buffer.indexOf("\n\n")) !== -1) {
                        const eventText = buffer.slice(0, eventEnd);
                        buffer = buffer.slice(eventEnd + 2);
                        // Parse SSE event
                        const lines = eventText.split("\n");
                        for (const line of lines) {
                            if (line.startsWith("data: ")) {
                                const data = line.slice(6);
                                try {
                                    const envelope = JSON.parse(data);
                                    if (envelope.error) {
                                        const code = (envelope.error.code || "internal");
                                        const msg = envelope.error.message || "Unknown error";
                                        emitRpcError(service, method, code, msg, emitErrors);
                                        setStatus("error", new ServerError(code, msg, httpStatus, envelope.error.details));
                                        return;
                                    }
                                    // Response validation
                                    if (validateResponse && schemas?.[opId]?.response) {
                                        const schema = schemas[opId].response;
                                        const result = await schema["~standard"].validate(envelope.result);
                                        if (result.issues) {
                                            const err = new ValidationError(opId, "response", result.issues);
                                            emitRpcError(service, method, "validation_error", err.message, emitErrors);
                                            setStatus("error", err);
                                            return;
                                        }
                                    }
                                    // Update data (notifies both listeners and dataListeners)
                                    setData(envelope.result);
                                }
                                catch (e) {
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
            }
            finally {
                reader.releaseLock();
            }
            // Stream ended cleanly - this is intentional completion, don't reconnect
            setStatus("completed");
        })().catch((err) => {
            // Silently ignore AbortError from cleanup (intentional disconnect)
            if (err.name === "AbortError") {
                setStatus("disconnected");
                return;
            }
            // Connection error - set error status and attempt reconnect
            setStatus("error", err);
            scheduleReconnect();
        }).finally(() => {
            connectionPromise = null;
            controller = null;
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
            controller = null;
        }
        connectionPromise = null;
    };
    // Create async iterator for for-await usage
    const createAsyncIterator = () => {
        const values = [];
        let resolveNext = null;
        let iteratorDone = false;
        const onData = (value) => {
            if (resolveNext) {
                resolveNext({ done: false, value });
                resolveNext = null;
            }
            else {
                values.push(value);
            }
        };
        // Also listen for errors/completion via status changes
        const onStatus = (result) => {
            if (result.status === "error" || result.status === "disconnected") {
                iteratorDone = true;
                if (resolveNext) {
                    resolveNext({ done: true, value: undefined });
                    resolveNext = null;
                }
            }
        };
        dataListeners.add(onData);
        listeners.add(onStatus);
        if (dataListeners.size === 1 && listeners.size === 1) {
            connect();
        }
        return {
            async next() {
                if (values.length > 0) {
                    return { done: false, value: values.shift() };
                }
                if (iteratorDone) {
                    return { done: true, value: undefined };
                }
                return new Promise((resolve) => {
                    resolveNext = resolve;
                });
            },
            async return() {
                dataListeners.delete(onData);
                listeners.delete(onStatus);
                if (dataListeners.size === 0 && listeners.size === 0) {
                    disconnect();
                }
                return { done: true, value: undefined };
            },
        };
    };
    return {
        [Symbol.asyncIterator]() {
            return createAsyncIterator();
        },
        subscribe(listener) {
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
                }
            };
        },
        getSnapshot,
    };
}
/**
 * Helper to create a SubscriptionResult from current state.
 */
function makeSubscriptionResult(status, data, error, statusUpdatedAt, dataUpdatedAt) {
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
 * Creates an Atom client for synchronized state subscriptions.
 * Unlike streams, atoms represent current state - subscribers get the
 * current value immediately, then updates as they occur.
 *
 * Follows the external store contract (subscribe + getSnapshot) for
 * compatibility with React's useSyncExternalStore and similar patterns.
 */
function createAtomClient(opId, service, method, meta, config, fetchFn, schemas, validateRequest, validateResponse, emitErrors) {
    // Combined state
    let currentData = undefined;
    let currentStatus = "disconnected";
    let currentError = undefined;
    let statusUpdatedAt = Date.now();
    let dataUpdatedAt = undefined;
    // Listeners get notified on any state change
    const listeners = new Set();
    const getSnapshot = () => {
        return makeSubscriptionResult(currentStatus, currentData, currentError, statusUpdatedAt, dataUpdatedAt);
    };
    const notify = () => {
        const result = getSnapshot();
        listeners.forEach((listener) => listener(result));
    };
    const setStatus = (status, error) => {
        currentStatus = status;
        currentError = error;
        statusUpdatedAt = Date.now();
        notify();
    };
    const setData = (data) => {
        currentData = data;
        dataUpdatedAt = Date.now();
        notify();
    };
    // Single shared connection
    let controller = null;
    let connectionPromise = null;
    let reconnectAttempt = 0;
    let reconnectTimeout = null;
    // Track if we've ever successfully connected (for connecting vs reconnecting)
    let hasConnected = false;
    const scheduleReconnect = () => {
        // Only reconnect if we still have listeners
        if (listeners.size === 0)
            return;
        reconnectAttempt++;
        // Exponential backoff: 100ms, 200ms, 400ms, 800ms, 1600ms, max 3000ms
        const delay = Math.min(100 * Math.pow(2, reconnectAttempt - 1), 3000);
        reconnectTimeout = setTimeout(() => {
            reconnectTimeout = null;
            if (listeners.size > 0) {
                connect();
            }
        }, delay);
    };
    const connect = () => {
        if (connectionPromise)
            return connectionPromise;
        controller = new AbortController();
        setStatus(hasConnected ? "reconnecting" : "connecting");
        connectionPromise = (async () => {
            const req = {};
            const headers = config.headers ? config.headers() : {};
            const url = (config.baseUrl || "") + meta.path;
            const fetchOptions = {
                method: "POST",
                headers: {
                    ...headers,
                    "Content-Type": "application/json",
                    Accept: "text/event-stream",
                },
                body: JSON.stringify(req),
                signal: controller.signal,
            };
            let res;
            try {
                res = await fetchFn(url, fetchOptions);
            }
            catch (e) {
                if (e.name === "AbortError") {
                    setStatus("disconnected");
                    return;
                }
                const msg = e instanceof Error ? e.message : "Network error";
                emitRpcError(service, method, "network_error", msg, emitErrors);
                setStatus("error", new TransportError(msg, 0, e));
                return;
            }
            const httpStatus = res.status;
            // Check for non-SSE error response
            const contentType = res.headers.get("Content-Type") || "";
            if (!contentType.includes("text/event-stream")) {
                let rawBody = "";
                try {
                    rawBody = await res.text();
                    const envelope = JSON.parse(rawBody);
                    if (envelope.error) {
                        const code = (envelope.error.code || "internal");
                        const msg = envelope.error.message || "Unknown error";
                        emitRpcError(service, method, code, msg, emitErrors);
                        setStatus("error", new ServerError(code, msg, httpStatus, envelope.error.details));
                        return;
                    }
                }
                catch (e) {
                    if (e instanceof ServerError) {
                        setStatus("error", e);
                        return;
                    }
                    const msg = res.statusText || "Failed to establish atom subscription";
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
                    if (done)
                        break;
                    buffer += decoder.decode(value, { stream: true });
                    // Parse complete SSE events from buffer
                    let eventEnd;
                    while ((eventEnd = buffer.indexOf("\n\n")) !== -1) {
                        const eventText = buffer.slice(0, eventEnd);
                        buffer = buffer.slice(eventEnd + 2);
                        // Parse SSE event
                        const lines = eventText.split("\n");
                        for (const line of lines) {
                            if (line.startsWith("data: ")) {
                                const data = line.slice(6);
                                try {
                                    const envelope = JSON.parse(data);
                                    if (envelope.error) {
                                        const code = (envelope.error.code || "internal");
                                        const msg = envelope.error.message || "Unknown error";
                                        emitRpcError(service, method, code, msg, emitErrors);
                                        setStatus("error", new ServerError(code, msg, httpStatus, envelope.error.details));
                                        return;
                                    }
                                    // Response validation
                                    if (validateResponse && schemas?.[opId]?.response) {
                                        const schema = schemas[opId].response;
                                        const result = await schema["~standard"].validate(envelope.result);
                                        if (result.issues) {
                                            const err = new ValidationError(opId, "response", result.issues);
                                            emitRpcError(service, method, "validation_error", err.message, emitErrors);
                                            setStatus("error", err);
                                            return;
                                        }
                                    }
                                    // Update data
                                    setData(envelope.result);
                                }
                                catch (e) {
                                    if (e instanceof ServerError || e instanceof ValidationError) {
                                        setStatus("error", e);
                                        return;
                                    }
                                    emitRpcError(service, method, "transport_error", "Failed to parse atom event", emitErrors);
                                    setStatus("error", new TransportError("Failed to parse atom event", httpStatus, e, data));
                                    return;
                                }
                            }
                        }
                    }
                }
            }
            finally {
                reader.releaseLock();
            }
            // Atom connection closed unexpectedly - reconnect
            // (Atoms represent persistent server state, they shouldn't end cleanly)
            setStatus("reconnecting");
            scheduleReconnect();
        })().catch((err) => {
            // Silently ignore AbortError from cleanup (intentional disconnect)
            if (err.name === "AbortError") {
                setStatus("disconnected");
                return;
            }
            // Connection error - set error status and attempt reconnect
            setStatus("error", err);
            scheduleReconnect();
        }).finally(() => {
            connectionPromise = null;
            controller = null;
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
            controller = null;
        }
        connectionPromise = null;
    };
    return {
        subscribe(listener) {
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
