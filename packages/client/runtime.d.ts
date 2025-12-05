import type { Error as TygorErrorEnvelope, ErrorCode } from "./generated/types.js";
export type { ErrorCode } from "./generated/types.js";
/**
 * TygorError is the base class for all tygor client errors.
 * Use instanceof to narrow to ServerError or TransportError.
 */
export declare abstract class TygorError extends Error {
    abstract readonly kind: "server" | "transport";
    constructor(message: string, options?: ErrorOptions);
}
/**
 * ServerError represents an application-level error returned by the tygor server.
 * These have a structured code, message, and optional details.
 */
export declare class ServerError extends TygorError {
    readonly kind: "server";
    code: ErrorCode;
    details?: Record<string, unknown>;
    httpStatus: number;
    constructor(code: ErrorCode, message: string, httpStatus: number, details?: Record<string, unknown>);
}
/**
 * TransportError represents a transport-level error (proxy, network, malformed response).
 * These occur when the response is not a valid tygor envelope.
 */
export declare class TransportError extends TygorError {
    readonly kind: "transport";
    httpStatus: number;
    rawBody?: string;
    constructor(message: string, httpStatus: number, cause?: unknown, rawBody?: string);
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
export type Response<T> = {
    result: T;
    error?: never;
} | {
    result?: never;
    error: TygorErrorEnvelope;
};
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
    readonly path?: readonly (PropertyKey | {
        readonly key: PropertyKey;
    })[];
}
/**
 * ValidationError is thrown when client-side schema validation fails.
 */
export declare class ValidationError extends Error {
    readonly kind: "validation";
    issues: readonly StandardSchemaIssue[];
    direction: "request" | "response";
    endpoint: string;
    constructor(endpoint: string, direction: "request" | "response", issues: readonly StandardSchemaIssue[]);
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
export interface ServiceRegistry<Manifest extends Record<string, any>> {
    manifest: Manifest;
    metadata: Record<string, {
        path: string;
        primitive: "query" | "exec" | "stream" | "atom";
    }>;
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
 * // React - subscribe to latest value (same as Atom)
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
/**
 * SubscriptionResult is the combined state of a subscription (Atom or Stream).
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
 * Atom represents a server-side state that syncs to clients in real-time.
 *
 * The subscribe method follows the external store contract expected by
 * React's useSyncExternalStore and similar patterns in other frameworks.
 *
 * @example
 * // Vanilla JS
 * const unsubscribe = client.Message.State.subscribe((result) => {
 *   console.log(result.status, result.data);
 * });
 *
 * // React with useSyncExternalStore
 * const result = useSyncExternalStore(
 *   atom.subscribe,
 *   atom.getSnapshot,
 *   atom.getSnapshot
 * );
 *
 * // Or use a selector for granular updates
 * const data = useSyncExternalStore(
 *   atom.subscribe,
 *   () => atom.getSnapshot().data,
 *   () => atom.getSnapshot().data
 * );
 */
export interface Atom<T> {
    /** Subscribe to all atom state changes. Returns unsubscribe function. */
    subscribe(listener: (result: SubscriptionResult<T>) => void): () => void;
    /** Get the current snapshot synchronously. */
    getSnapshot(): SubscriptionResult<T>;
}
export declare function createClient<Manifest extends Record<string, any>>(registry: ServiceRegistry<Manifest>, config?: ClientConfig): Client<Manifest>;
type ServiceName<K> = K extends `${infer S}.${string}` ? S : never;
type IsOptionalRequest<T> = {} extends T ? true : false;
type ServiceMethods<M, S extends string> = {
    [K in keyof M as K extends `${S}.${infer Method}` ? Method : never]: M[K] extends {
        req: infer Req;
        res: infer Res;
        primitive: "stream";
    } ? IsOptionalRequest<Req> extends true ? (req?: Req, options?: StreamOptions) => Stream<Res> : (req: Req, options?: StreamOptions) => Stream<Res> : M[K] extends {
        res: infer Res;
        primitive: "atom";
    } ? Atom<Res> : M[K] extends {
        req: infer Req;
        res: infer Res;
    } ? IsOptionalRequest<Req> extends true ? (req?: Req) => Promise<Res> : (req: Req) => Promise<Res> : never;
};
export type Client<M> = {
    [K in keyof M as ServiceName<K>]: ServiceMethods<M, ServiceName<K>>;
};
//# sourceMappingURL=runtime.d.ts.map