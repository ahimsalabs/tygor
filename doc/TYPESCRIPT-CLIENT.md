<!-- snippet-lint-disable -->

# Tygor TypeScript Client Specification

**Version:** 1.0
**Status:** Draft

## 1. Introduction

This document specifies the TypeScript client implementation for the **tygor** RPC system. The client provides type-safe access to tygor services with zero boilerplate and full IDE autocomplete support.

For wire protocol details, see **PROTOCOL.md**.
For Go server implementation details, see **GO-IMPLEMENTATION.md**.

---

## 2. Design Principles

### 2.1 Zero Boilerplate

The client MUST NOT require generating individual method implementations. All RPC calls are resolved dynamically using ES6 Proxies.

### 2.2 Full Type Safety

The client MUST provide compile-time type checking for:
- Request parameters
- Response types
- Available services and methods

### 2.3 Simple API

The client syntax MUST be: `client.Service.Method(params)`

---

## 3. Generated Files

### 3.1 File Structure

The code generator MUST produce two files:

```
rpc/
├── types.ts      # TypeScript interfaces for all request/response types
└── manifest.ts   # Operation metadata and manifest
```

### 3.2 Types File (`types.ts`)

This file contains TypeScript interfaces corresponding to all Go structs used in registered handlers.

**Example:**
```typescript
// types.ts

/** Request parameters for listing news articles */
export interface ListNewsParams {
  limit?: number;
  offset?: number;
  tags?: string[];
}

/** A news article */
export interface News {
  id: number;
  title: string;
  body: string;
  createdAt: string; // ISO 8601 timestamp
  tags: string[];
}

export interface CreateNewsParams {
  title: string;
  body: string;
  tags?: string[];
}
```

**Requirements:**
- All types MUST be exported
- Optional Go pointer fields MUST be typed as `T | undefined` (default) or `T | null` (if configured)
- Timestamps MUST be typed as `string` with ISO 8601 format
- Comments from Go code SHOULD be preserved as JSDoc comments

### 3.3 Manifest File (`manifest.ts`)

This file exports the RPC manifest interface and a registry object containing type metadata and runtime metadata.

**Structure:**
```typescript
// manifest.ts
import { ServiceRegistry } from '@tygor/client';
import * as types from './types';

/** Type-safe RPC manifest mapping operation IDs to request/response types */
export interface Manifest {
  "News.List": {
    req: types.ListNewsParams;
    res: types.News[];
  };
  "News.Create": {
    req: types.CreateNewsParams;
    res: types.News;
  };
}

/** Runtime metadata for all RPC operations */
const metadata = {
  "News.List": { path: "/News/List", primitive: "query" },
  "News.Create": { path: "/News/Create", primitive: "exec" },
} as const;

/** Registry combining type manifest and runtime metadata */
export const registry: ServiceRegistry<Manifest> = {
  manifest: {} as Manifest,
  metadata,
};
```

**Requirements:**
- `Manifest` interface MUST map operation IDs to their request/response types
- `metadata` constant MUST provide runtime access to path and primitive type
- `primitive` field indicates the communication pattern: `"query"`, `"exec"`, or `"stream"`
- `registry` object MUST combine the manifest type and metadata for client construction
- Operation IDs MUST follow the format `"{Service}.{Method}"`

---

## 4. Client Runtime

### 4.1 Client Implementation

The client MUST be implemented using ES6 Proxies to dynamically resolve method calls.

**Example Implementation:**
```typescript
// client.ts
import { Manifest, Metadata } from './rpc/manifest';

type UnwrapPromise<T> = T extends Promise<infer U> ? U : T;

export type RPCClient = {
  [Service in keyof Manifest as Service extends `${infer S}.${string}` ? S : never]: {
    [Method in keyof Manifest as Method extends `${Service}.${infer M}` ? M : never]:
      (req: Manifest[Method]['req']) => Promise<Manifest[Method]['res']>
  }
};

export interface ClientConfig {
  baseURL: string;
  fetch?: typeof fetch;
  headers?: Record<string, string>;
  onError?: (error: TygorError) => void;
}

export abstract class TygorError extends Error {
  abstract readonly kind: "rpc" | "transport";
}

export class RPCError extends TygorError {
  readonly kind = "rpc" as const;
  code: string;
  details?: Record<string, any>;
  httpStatus: number;

  constructor(code: string, message: string, httpStatus: number, details?: Record<string, any>) {
    super(message);
    this.name = "RPCError";
    this.code = code;
    this.httpStatus = httpStatus;
    this.details = details;
  }
}

export class TransportError extends TygorError {
  readonly kind = "transport" as const;
  httpStatus: number;
  rawBody?: string;

  constructor(message: string, httpStatus: number, rawBody?: string) {
    super(message);
    this.name = "TransportError";
    this.httpStatus = httpStatus;
    this.rawBody = rawBody;
  }
}

export function createClient(config: ClientConfig): RPCClient {
  const { baseURL, fetch: customFetch = globalThis.fetch, headers = {} } = config;

  return new Proxy({} as RPCClient, {
    get(_, service: string) {
      return new Proxy({}, {
        get(_, method: string) {
          return async (req: any) => {
            const operationId = `${service}.${method}` as keyof typeof RPCMetadata;
            const metadata = RPCMetadata[operationId];

            if (!metadata) {
              throw new Error(`Unknown operation: ${operationId}`);
            }

            const url = new URL(metadata.path, baseURL);
            const options: RequestInit = {
              method: metadata.method,
              headers: {
                'Accept': 'application/json',
                ...headers,
              },
            };

            if (metadata.method === 'GET') {
              // Encode request as query parameters
              if (req) {
                Object.entries(req).forEach(([key, value]) => {
                  if (Array.isArray(value)) {
                    value.forEach(v => url.searchParams.append(key, String(v)));
                  } else if (value !== undefined && value !== null) {
                    url.searchParams.set(key, String(value));
                  }
                });
              }
            } else {
              // Encode request as JSON body
              options.headers = {
                ...options.headers,
                'Content-Type': 'application/json',
              };
              options.body = JSON.stringify(req);
            }

            const response = await customFetch(url.toString(), options);

            // Try to parse as JSON
            let rawBody: string | undefined;
            let envelope: any;
            try {
              rawBody = await response.clone().text();
              envelope = JSON.parse(rawBody);
            } catch {
              // JSON parse failed - this is a transport error (proxy HTML page, etc.)
              throw new TransportError(
                response.statusText || "Failed to parse response",
                response.status,
                rawBody?.slice(0, 1000) // Truncate for sanity
              );
            }

            // Handle malformed or null envelope
            if (!envelope || typeof envelope !== "object") {
              throw new TransportError(
                "Invalid response format",
                response.status,
                rawBody?.slice(0, 1000)
              );
            }

            // Validate envelope has expected structure
            if (!("result" in envelope) && !("error" in envelope)) {
              throw new TransportError(
                "Invalid response format: missing result or error field",
                response.status,
                rawBody?.slice(0, 1000)
              );
            }

            // Check for error in envelope - this is an application-level error
            if (envelope.error) {
              const error = new RPCError(
                envelope.error.code || "unknown",
                envelope.error.message || "Unknown error",
                response.status,
                envelope.error.details
              );
              if (config.onError) {
                config.onError(error);
              }
              throw error;
            }

            // Return the unwrapped result
            return envelope.result;
          };
        },
      });
    },
  }) as RPCClient;
}
```

### 4.2 Client API Requirements

The client MUST:
- ✅ Use double Proxy pattern (service → method → function)
- ✅ Resolve operation metadata from `RPCMetadata`
- ✅ Serialize GET requests as query parameters (with array repeat format)
- ✅ Serialize POST requests as JSON body
- ✅ Parse successful responses as JSON
- ✅ Parse error responses and throw structured errors
- ✅ Support custom `fetch` implementation (for Node.js, testing, etc.)
- ✅ Support custom headers (authentication, etc.)
- ✅ Distinguish between application-level errors (RPCError) and transport-level errors (TransportError)

### 4.3 Error Types

The client defines a type hierarchy for errors:

#### 4.3.1 TygorError (Base Class)

All errors thrown by the tygor client extend `TygorError`:

```typescript
abstract class TygorError extends Error {
  abstract readonly kind: "rpc" | "transport";
}
```

The `kind` property is a discriminant that allows type narrowing.

#### 4.3.2 RPCError (Application-Level Errors)

Thrown when the server returns a valid error envelope: `{"error": {...}}`.

```typescript
class RPCError extends TygorError {
  readonly kind = "rpc" as const;
  code: string;           // Error code (e.g., "invalid_argument", "not_found")
  message: string;        // Human-readable error message
  httpStatus: number;     // HTTP status code (e.g., 400, 404)
  details?: Record<string, any>; // Optional structured details
}
```

**When RPCError is thrown:**
- Server returned `{"error": {"code": "...", "message": "..."}}`
- The response is a valid tygor error envelope
- This represents an application-level error (validation, authorization, business logic)

#### 4.3.3 TransportError (Transport-Level Errors)

Thrown when the response is not a valid tygor envelope:

```typescript
class TransportError extends TygorError {
  readonly kind = "transport" as const;
  message: string;        // Error description
  httpStatus: number;     // HTTP status code
  rawBody?: string;       // First 1000 chars of response body (for debugging)
}
```

**When TransportError is thrown:**
- JSON parse failed (e.g., HTML error page from proxy)
- Response is null or not an object
- Response missing both `result` and `error` fields
- This represents infrastructure/network issues, not application logic

#### 4.3.4 Error Classification Examples

| Response | Error Type | Reason |
|----------|-----------|--------|
| `{"error": {"code": "not_found", "message": "User not found"}}` | RPCError | Valid error envelope |
| `<html><body>502 Bad Gateway</body></html>` | TransportError | Invalid JSON (proxy HTML) |
| `{"status": "error", "msg": "Failed"}` | TransportError | Missing `result`/`error` fields |
| `null` | TransportError | Null response |
| Network timeout | Native Error | Not a tygor error |

---

## 5. Client Usage

### 5.1 Basic Usage

```typescript
import { createClient } from './client';
import type { RPCClient } from './client';

const client: RPCClient = createClient({
  baseURL: 'https://api.example.com',
});

// Type-safe RPC calls
const news = await client.News.List({ limit: 10, offset: 0 });
//    ^? News[]

const created = await client.News.Create({
  title: 'Hello World',
  body: 'This is a test article',
  tags: ['test', 'typescript'],
});
//    ^? News
```

### 5.2 Authentication

```typescript
const client = createClient({
  baseURL: 'https://api.example.com',
  headers: {
    'Authorization': `Bearer ${token}`,
  },
});
```

### 5.3 Error Handling

The client throws two types of errors, both extending `TygorError`:

#### 5.3.1 RPCError (Application-Level Errors)

These are errors returned by the tygor server in the `{"error": {...}}` envelope. They have a structured error code, message, and optional details.

```typescript
import { TygorError, RPCError, TransportError } from './client';

try {
  await client.News.Create({ title: '', body: '' });
} catch (error) {
  // Type narrowing with instanceof
  if (error instanceof RPCError) {
    console.error('RPC Error:', error.code, error.message);
    console.error('HTTP Status:', error.httpStatus);
    console.error('Details:', error.details);

    if (error.code === 'invalid_argument') {
      // Handle validation error
    } else if (error.code === 'unauthenticated') {
      // Redirect to login
    }
  } else if (error instanceof TransportError) {
    console.error('Transport Error:', error.message);
    console.error('HTTP Status:', error.httpStatus);
    console.error('Raw Body:', error.rawBody);
  }
}
```

#### 5.3.2 TransportError (Transport-Level Errors)

These occur when the response is not a valid tygor envelope:
- Invalid JSON (e.g., HTML error pages from proxies)
- Null or non-object responses
- Missing `result`/`error` fields

```typescript
try {
  await client.News.List({ limit: 10 });
} catch (error) {
  if (error instanceof TransportError) {
    // Proxy returned HTML, network error, malformed response, etc.
    console.error('Transport error:', error.message);
    console.error('Status code:', error.httpStatus);

    // rawBody is truncated to 1000 chars for debugging
    if (error.rawBody) {
      console.error('Response preview:', error.rawBody);
    }
  }
}
```

#### 5.3.3 Using the `kind` Discriminant

You can also use the `kind` property to discriminate between error types:

```typescript
try {
  await client.News.Create(params);
} catch (error) {
  if (error instanceof TygorError) {
    if (error.kind === "rpc") {
      // TypeScript narrows to RPCError
      console.log('RPC error code:', error.code);
    } else {
      // TypeScript narrows to TransportError
      console.log('Transport error body:', error.rawBody);
    }
  }
}
```

### 5.4 Custom Error Handler

The `onError` callback is invoked for all errors (both RPCError and TransportError) before they are thrown. This is useful for logging, analytics, or showing toast notifications.

```typescript
const client = createClient({
  baseURL: 'https://api.example.com',
  onError: (error: TygorError) => {
    // Log to monitoring service
    if (error instanceof RPCError) {
      console.error('[RPC Error]', error.code, error.message, error.details);

      // Show toast notification for specific error codes
      if (error.code === 'unauthenticated') {
        redirectToLogin();
      } else if (error.code === 'permission_denied') {
        showToast('You do not have permission to perform this action');
      }
    } else if (error instanceof TransportError) {
      console.error('[Transport Error]', error.message, error.httpStatus);

      // Log to error tracking service with raw body for debugging
      trackError('transport_error', {
        message: error.message,
        status: error.httpStatus,
        preview: error.rawBody,
      });
    }
  },
});
```

### 5.5 Server-Side Usage (Node.js)

```typescript
import { createClient } from './client';
import fetch from 'node-fetch';

const client = createClient({
  baseURL: 'http://localhost:8080',
  fetch: fetch as any, // Use node-fetch
});
```

---

## 6. Framework Integration

### 6.1 React Integration

**React Hook:**
```typescript
// hooks/useRPC.ts
import { useQuery, useMutation } from '@tanstack/react-query';
import { client } from '../client';

export function useNewsList(params: { limit: number; offset: number }) {
  return useQuery({
    queryKey: ['news', 'list', params],
    queryFn: () => client.News.List(params),
  });
}

export function useNewsCreate() {
  return useMutation({
    mutationFn: (params) => client.News.Create(params),
  });
}
```

**Usage:**
```tsx
function NewsPage() {
  const { data: news, isLoading } = useNewsList({ limit: 10, offset: 0 });
  const createNews = useNewsCreate();

  if (isLoading) return <div>Loading...</div>;

  return (
    <div>
      {news?.map(article => (
        <div key={article.id}>{article.title}</div>
      ))}
      <button onClick={() => createNews.mutate({ title: 'New', body: 'test' })}>
        Create
      </button>
    </div>
  );
}
```

### 6.2 Vue Integration

```typescript
// composables/useRPC.ts
import { ref } from 'vue';
import { client } from '../client';

export function useNewsList() {
  const news = ref([]);
  const loading = ref(false);

  async function fetch(params: { limit: number; offset: number }) {
    loading.value = true;
    try {
      news.value = await client.News.List(params);
    } finally {
      loading.value = false;
    }
  }

  return { news, loading, fetch };
}
```

### 6.3 SvelteKit Integration

```typescript
// routes/news/+page.ts
import { client } from '$lib/client';

export async function load() {
  const news = await client.News.List({ limit: 10, offset: 0 });
  return { news };
}
```

---

## 7. Advanced Features

### 7.1 Request Interceptors

```typescript
export function createClient(config: ClientConfig) {
  // Wrapper around fetch for interceptors
  const wrappedFetch = async (url: string, options: RequestInit) => {
    // Before request
    console.log('[RPC Request]', url, options);

    const response = await config.fetch(url, options);

    // After response
    console.log('[RPC Response]', response.status);

    return response;
  };

  // Use wrappedFetch in proxy implementation
  // ...
}
```

### 7.2 Automatic Retries

```typescript
async function fetchWithRetry(
  url: string,
  options: RequestInit,
  retries = 3
): Promise<Response> {
  try {
    return await fetch(url, options);
  } catch (error) {
    if (retries > 0) {
      await new Promise(resolve => setTimeout(resolve, 1000));
      return fetchWithRetry(url, options, retries - 1);
    }
    throw error;
  }
}

const client = createClient({
  baseURL: 'https://api.example.com',
  fetch: (url, options) => fetchWithRetry(url, options),
});
```

### 7.3 Request Caching

For GET requests, leverage browser caching or implement custom caching:

```typescript
const cache = new Map<string, { data: any; expires: number }>();

const client = createClient({
  baseURL: 'https://api.example.com',
  fetch: async (url, options) => {
    if (options.method === 'GET') {
      const cached = cache.get(url);
      if (cached && Date.now() < cached.expires) {
        return new Response(JSON.stringify(cached.data), {
          status: 200,
          headers: { 'Content-Type': 'application/json' },
        });
      }
    }

    const response = await fetch(url, options);

    if (response.ok && options.method === 'GET') {
      const data = await response.clone().json();
      cache.set(url, { data, expires: Date.now() + 60000 });
    }

    return response;
  },
});
```

---

## 8. Testing

### 8.1 Mocking the Client

```typescript
// __tests__/news.test.ts
import { createClient } from '../client';
import type { RPCClient } from '../client';

const mockFetch = vi.fn();

const client: RPCClient = createClient({
  baseURL: 'http://test',
  fetch: mockFetch,
});

test('fetches news list', async () => {
  mockFetch.mockResolvedValueOnce({
    ok: true,
    json: async () => [{ id: 1, title: 'Test' }],
  });

  const news = await client.News.List({ limit: 10 });

  expect(news).toEqual([{ id: 1, title: 'Test' }]);
  expect(mockFetch).toHaveBeenCalledWith(
    'http://test/News/List?limit=10',
    expect.objectContaining({ method: 'GET' })
  );
});
```

### 8.2 MSW (Mock Service Worker) Integration

```typescript
// mocks/handlers.ts
import { http, HttpResponse } from 'msw';

export const handlers = [
  http.get('/News/List', () => {
    return HttpResponse.json([
      { id: 1, title: 'Mocked News', body: 'Test', tags: [] },
    ]);
  }),

  http.post('/News/Create', async ({ request }) => {
    const body = await request.json();
    return HttpResponse.json({
      id: 999,
      ...body,
      createdAt: new Date().toISOString(),
    });
  }),
];
```

---

## 9. Type Generation Configuration

The Go server's `tygorgen.Config` affects the generated TypeScript types. Here's how each option impacts the client:

### 9.1 Optional Type Handling

```go
// Go configuration (default)
tygorgen.FromApp(app).
    OptionalType("undefined").
    ToDir("./client/src/rpc")
```

```typescript
// Generated TypeScript
export interface UpdateUserParams {
  name?: string;      // string | undefined
  email?: string;     // string | undefined
}
```

vs.

```go
tygorgen.FromApp(app).
    OptionalType("null").
    ToDir("./client/src/rpc")
```

```typescript
export interface UpdateUserParams {
  name: string | null;
  email: string | null;
}
```

**Recommendation:** Use `"undefined"` (default) for better JSON semantics.

### 9.2 Enum Style

```go
// Go
type Status string
const (
    StatusDraft     Status = "draft"
    StatusPublished Status = "published"
)
```

With `EnumStyle: "union"` (default):
```typescript
export type Status = "draft" | "published";
```

With `EnumStyle: "enum"`:
```typescript
export enum Status {
  Draft = "draft",
  Published = "published",
}
```

### 9.3 Custom Type Mappings

```go
tygorgen.FromApp(app).
    TypeMapping("uuid.UUID", "string").
    TypeMapping("decimal.Decimal", "number").
    TypeMapping("time.Time", "Date"). // Custom: use Date objects instead of strings
    ToDir("./client/src/rpc")
```

**Note:** Using `Date` for `time.Time` requires custom JSON parsing.

### 9.4 Frontmatter (Branded Types)

```go
tygorgen.FromApp(app).
    Frontmatter(`
export type UserID = number & { __brand: 'UserID' };
export type DateTime = string & { __brand: 'DateTime' };
`).
    ToDir("./client/src/rpc")
```

Enables compile-time type safety for primitives:
```typescript
function getUser(id: UserID) { ... }

const id: number = 123;
getUser(id); // Type error: number is not assignable to UserID

const userId = 123 as UserID;
getUser(userId); // OK
```

---

## 10. Compliance Checklist

A compliant TypeScript client MUST:

- ✅ Use ES6 Proxy for dynamic method resolution
- ✅ Provide type-safe `client.Service.Method(params)` syntax
- ✅ Serialize GET requests as query parameters (with repeat arrays)
- ✅ Serialize POST requests as JSON body
- ✅ Parse JSON responses and validate envelope structure
- ✅ Throw `RPCError` for application-level errors (`{"error": {...}}` envelope)
- ✅ Throw `TransportError` for invalid JSON, null responses, or missing result/error fields
- ✅ Provide `TygorError` base class with `kind` discriminant
- ✅ Include `httpStatus` in both error types
- ✅ Truncate `rawBody` to 1000 chars in `TransportError` for debugging
- ✅ Support custom `fetch` implementation
- ✅ Support custom headers
- ✅ Provide full TypeScript type definitions from manifest

---

## 11. Complete Example

**Generated Files:**
```typescript
// rpc/types.ts
export interface ListNewsParams {
  limit?: number;
  offset?: number;
}

export interface News {
  id: number;
  title: string;
  body: string;
}
```

```typescript
// rpc/manifest.ts
import { ServiceRegistry } from '@tygor/client';
import * as types from './types';

export interface Manifest {
  "News.List": {
    req: types.ListNewsParams;
    res: types.News[];
  };
}

const metadata = {
  "News.List": { path: "/News/List", primitive: "query" },
} as const;

export const registry: ServiceRegistry<Manifest> = {
  manifest: {} as Manifest,
  metadata,
};
```

**Application Code:**
```typescript
// client.ts
import { createClient } from '@tygor/client';
import { registry } from './rpc/manifest';

export const client = createClient(registry, {
  baseUrl: import.meta.env.VITE_API_URL,
  headers: () => ({
    'Authorization': `Bearer ${getToken()}`,
  }),
});

// app.ts
async function loadNews() {
  try {
    const news = await client.News.List({ limit: 10 });
    console.log('News:', news);
  } catch (error) {
    console.error('Failed to load news:', error);
  }
}
```
