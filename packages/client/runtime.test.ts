import { describe, test, expect, mock } from "bun:test";
import { createClient, TygorError, ServerError, TransportError, ServiceRegistry, Atom, Stream, SubscriptionResult } from "./runtime";

// Helper to create a mock response
function mockResponse(status: number, body: any, statusText = "") {
  const bodyStr = typeof body === "string" ? body : JSON.stringify(body);
  return {
    ok: status >= 200 && status < 300,
    status,
    statusText,
    clone: function() { return this; },
    text: async () => bodyStr,
  };
}

describe("TygorError hierarchy", () => {
  test("ServerError has correct properties", () => {
    const error = new ServerError("not_found", "Resource not found", 404);
    expect(error.name).toBe("ServerError");
    expect(error.kind).toBe("server");
    expect(error.code).toBe("not_found");
    expect(error.message).toBe("Resource not found");
    expect(error.httpStatus).toBe(404);
    expect(error.details).toBeUndefined();
    expect(error).toBeInstanceOf(TygorError);
    expect(error).toBeInstanceOf(ServerError);
  });

  test("ServerError with details", () => {
    const error = new ServerError("validation_error", "Invalid input", 400, {
      field: "email",
      reason: "invalid format",
    });
    expect(error.code).toBe("validation_error");
    expect(error.httpStatus).toBe(400);
    expect(error.details).toEqual({ field: "email", reason: "invalid format" });
  });

  test("TransportError has correct properties", () => {
    const error = new TransportError("Bad Gateway", 502, undefined, "<html>...</html>");
    expect(error.name).toBe("TransportError");
    expect(error.kind).toBe("transport");
    expect(error.message).toBe("Bad Gateway");
    expect(error.httpStatus).toBe(502);
    expect(error.rawBody).toBe("<html>...</html>");
    expect(error).toBeInstanceOf(TygorError);
    expect(error).toBeInstanceOf(TransportError);
  });

  test("can discriminate error types", () => {
    const serverError: TygorError = new ServerError("not_found", "Not found", 404);
    const transportError: TygorError = new TransportError("Bad Gateway", 502);

    // instanceof narrowing
    if (serverError instanceof ServerError) {
      expect(serverError.code).toBe("not_found");
    }
    if (transportError instanceof TransportError) {
      expect(transportError.httpStatus).toBe(502);
    }

    // kind discriminant
    expect(serverError.kind).toBe("server");
    expect(transportError.kind).toBe("transport");
  });
});

describe("createClient", () => {
  const mockMetadata = {
    "Test.Get": { path: "/test/get", primitive: "query" as const },
    "Test.Post": { path: "/test/post", primitive: "exec" as const },
    "Test.Put": { path: "/test/put", primitive: "exec" as const },
    "Test.Patch": { path: "/test/patch", primitive: "exec" as const },
    "Test.Delete": { path: "/test/delete", primitive: "exec" as const },
  };

  type TestManifest = {
    "Test.Get": { req: { id: string }; res: { data: string } };
    "Test.Post": { req: { name: string }; res: { created: boolean } };
    "Test.Put": { req: { id: string; name: string }; res: { updated: boolean } };
    "Test.Patch": { req: { id: string; name?: string }; res: { updated: boolean } };
    "Test.Delete": { req: { id: string }; res: { deleted: boolean } };
  };

  const mockRegistry: ServiceRegistry<TestManifest> = {
    manifest: {} as TestManifest,
    metadata: mockMetadata,
  };

  test("successful query request", async () => {
    const mockFetch = mock(async () => mockResponse(200, { result: { data: "success" } }));

    const client = createClient(mockRegistry, {
      baseUrl: "http://localhost:8080",
      fetch: mockFetch,
    });

    const result = await client.Test.Get({ id: "123" });

    expect(result).toEqual({ data: "success" });
    expect(mockFetch).toHaveBeenCalledWith(
      "http://localhost:8080/test/get?id=123",
      expect.objectContaining({ method: "GET" })
    );
  });

  test("successful exec request", async () => {
    const mockFetch = mock(async () => mockResponse(200, { result: { created: true } }));

    const client = createClient(mockRegistry, {
      baseUrl: "http://localhost:8080",
      fetch: mockFetch,
    });

    const result = await client.Test.Post({ name: "test" });

    expect(result).toEqual({ created: true });
    expect(mockFetch).toHaveBeenCalledWith(
      "http://localhost:8080/test/post",
      expect.objectContaining({
        method: "POST",
        headers: expect.objectContaining({ "Content-Type": "application/json" }),
        body: JSON.stringify({ name: "test" }),
      })
    );
  });

  test("query request with custom headers", async () => {
    const mockFetch = mock(async () => mockResponse(200, { result: { data: "success" } }));

    const client = createClient(mockRegistry, {
      baseUrl: "http://localhost:8080",
      headers: () => ({ Authorization: "Bearer token123" }),
      fetch: mockFetch,
    });

    await client.Test.Get({ id: "123" });

    expect(mockFetch).toHaveBeenCalledWith(
      expect.any(String),
      expect.objectContaining({
        headers: expect.objectContaining({ Authorization: "Bearer token123" }),
      })
    );
  });

  test("uses globalThis.fetch when fetch option is not provided", async () => {
    const mockFetch = mock(async () => mockResponse(200, { result: { data: "success" } }));
    global.fetch = mockFetch as any;

    const client = createClient(mockRegistry, { baseUrl: "http://localhost:8080" });

    const result = await client.Test.Get({ id: "123" });

    expect(result).toEqual({ data: "success" });
  });

  test("error response with valid JSON error envelope (ServerError)", async () => {
    const mockFetch = mock(async () => mockResponse(400, {
      error: {
        code: "invalid_input",
        message: "Email is required",
        details: { field: "email" },
      },
    }, "Bad Request"));
    global.fetch = mockFetch as any;

    const client = createClient(mockRegistry, { baseUrl: "http://localhost:8080" });

    try {
      await client.Test.Get({ id: "123" });
      expect.unreachable("Should have thrown an error");
    } catch (e: any) {
      expect(e).toBeInstanceOf(ServerError);
      expect(e.kind).toBe("server");
      expect(e.code).toBe("invalid_input");
      expect(e.message).toBe("Email is required");
      expect(e.httpStatus).toBe(400);
      expect(e.details).toEqual({ field: "email" });
    }
  });

  test("transport error with null body", async () => {
    const mockFetch = mock(async () => mockResponse(404, null, "Not Found"));
    global.fetch = mockFetch as any;

    const client = createClient(mockRegistry, { baseUrl: "http://localhost:8080" });

    try {
      await client.Test.Get({ id: "123" });
      expect.unreachable("Should have thrown an error");
    } catch (e: any) {
      expect(e).toBeInstanceOf(TransportError);
      expect(e.kind).toBe("transport");
      expect(e.httpStatus).toBe(404);
      expect(e.message).toBe("Invalid response format");
    }
  });

  test("transport error with invalid JSON (HTML from proxy)", async () => {
    const htmlBody = "<!DOCTYPE html><html><body>502 Bad Gateway</body></html>";
    const mockFetch = mock(async () => ({
      ok: false,
      status: 502,
      statusText: "Bad Gateway",
      clone: function() { return this; },
      text: async () => htmlBody,
    }));
    global.fetch = mockFetch as any;

    const client = createClient(mockRegistry, { baseUrl: "http://localhost:8080" });

    try {
      await client.Test.Get({ id: "123" });
      expect.unreachable("Should have thrown an error");
    } catch (e: any) {
      expect(e).toBeInstanceOf(TransportError);
      expect(e.kind).toBe("transport");
      expect(e.httpStatus).toBe(502);
      expect(e.message).toBe("Bad Gateway");
      expect(e.rawBody).toBe(htmlBody);
    }
  });

  test("error response with partial error object (missing code)", async () => {
    const mockFetch = mock(async () => mockResponse(400, {
      error: { message: "Something went wrong" },
    }, "Bad Request"));
    global.fetch = mockFetch as any;

    const client = createClient(mockRegistry, { baseUrl: "http://localhost:8080" });

    try {
      await client.Test.Get({ id: "123" });
      expect.unreachable("Should have thrown an error");
    } catch (e: any) {
      expect(e).toBeInstanceOf(ServerError);
      expect(e.code).toBe("internal"); // fallback when server omits code
      expect(e.message).toBe("Something went wrong");
    }
  });

  test("error response with partial error object (missing message)", async () => {
    const mockFetch = mock(async () => mockResponse(403, {
      error: { code: "forbidden" },
    }, "Forbidden"));
    global.fetch = mockFetch as any;

    const client = createClient(mockRegistry, { baseUrl: "http://localhost:8080" });

    try {
      await client.Test.Get({ id: "123" });
      expect.unreachable("Should have thrown an error");
    } catch (e: any) {
      expect(e).toBeInstanceOf(ServerError);
      expect(e.code).toBe("forbidden");
      expect(e.message).toBe("Unknown error");
    }
  });

  test("throws error for unknown service method", () => {
    const client = createClient(mockRegistry, { baseUrl: "http://localhost:8080" });

    expect(() => {
      (client as any).Unknown.Method({ test: true });
    }).toThrow("Unknown service method: Unknown.Method");
  });

  test("query request handles array parameters", async () => {
    const mockFetch = mock(async () => mockResponse(200, { result: { data: "success" } }));
    global.fetch = mockFetch as any;

    const metadata = { "Test.Search": { path: "/test/search", primitive: "query" as const } };
    type SearchManifest = { "Test.Search": { req: { tags: string[] }; res: { data: string } } };
    const searchRegistry: ServiceRegistry<SearchManifest> = {
      manifest: {} as SearchManifest,
      metadata,
    };

    const client = createClient(searchRegistry, { baseUrl: "http://localhost:8080" });
    await client.Test.Search({ tags: ["foo", "bar"] });

    expect(mockFetch).toHaveBeenCalledWith(
      "http://localhost:8080/test/search?tags=foo&tags=bar",
      expect.any(Object)
    );
  });

  test("query request omits null and undefined parameters", async () => {
    const mockFetch = mock(async () => mockResponse(200, { result: { data: "success" } }));
    global.fetch = mockFetch as any;

    const metadata = { "Test.Query": { path: "/test/query", primitive: "query" as const } };
    type QueryManifest = {
      "Test.Query": { req: { id: string; optional?: string; nullable: string | null }; res: { data: string } };
    };
    const queryRegistry: ServiceRegistry<QueryManifest> = {
      manifest: {} as QueryManifest,
      metadata,
    };

    const client = createClient(queryRegistry, { baseUrl: "http://localhost:8080" });
    await client.Test.Query({ id: "123", optional: undefined, nullable: null });

    expect(mockFetch).toHaveBeenCalledWith(
      "http://localhost:8080/test/query?id=123",
      expect.any(Object)
    );
  });

  test("exec request with custom headers preserves Authorization header", async () => {
    const mockFetch = mock(async () => mockResponse(200, { result: { created: true } }));
    global.fetch = mockFetch as any;

    const client = createClient(mockRegistry, {
      baseUrl: "http://localhost:8080",
      headers: () => ({ Authorization: "Bearer token123" }),
    });

    await client.Test.Post({ name: "test" });

    expect(mockFetch).toHaveBeenCalledWith(
      "http://localhost:8080/test/post",
      expect.objectContaining({
        method: "POST",
        headers: expect.objectContaining({
          Authorization: "Bearer token123",
          "Content-Type": "application/json",
        }),
      })
    );
  });

  test("successful PUT request", async () => {
    const mockFetch = mock(async () => mockResponse(200, { result: { updated: true } }));
    global.fetch = mockFetch as any;

    const client = createClient(mockRegistry, { baseUrl: "http://localhost:8080" });
    const result = await client.Test.Put({ id: "123", name: "updated" });

    expect(result).toEqual({ updated: true });
  });

  test("successful PATCH request", async () => {
    const mockFetch = mock(async () => mockResponse(200, { result: { updated: true } }));
    global.fetch = mockFetch as any;

    const client = createClient(mockRegistry, { baseUrl: "http://localhost:8080" });
    const result = await client.Test.Patch({ id: "123", name: "patched" });

    expect(result).toEqual({ updated: true });
  });

  test("successful DELETE request", async () => {
    const mockFetch = mock(async () => mockResponse(200, { result: { deleted: true } }));
    global.fetch = mockFetch as any;

    const client = createClient(mockRegistry, { baseUrl: "http://localhost:8080" });
    const result = await client.Test.Delete({ id: "123" });

    expect(result).toEqual({ deleted: true });
  });

  test("query request parameters are sorted for consistent caching", async () => {
    const mockFetch = mock(async () => mockResponse(200, { result: { data: "success" } }));
    global.fetch = mockFetch as any;

    const metadata = { "Test.Search": { path: "/test/search", primitive: "query" as const } };
    type SearchManifest = {
      "Test.Search": { req: { name: string; id: string; limit: number }; res: { data: string } };
    };
    const searchRegistry: ServiceRegistry<SearchManifest> = {
      manifest: {} as SearchManifest,
      metadata,
    };

    const client = createClient(searchRegistry, { baseUrl: "http://localhost:8080" });

    await client.Test.Search({ name: "test", id: "123", limit: 10 });
    const url1 = mockFetch.mock.calls[0][0];
    mockFetch.mockClear();

    await client.Test.Search({ limit: 10, id: "123", name: "test" });
    const url2 = mockFetch.mock.calls[0][0];

    expect(url1).toBe(url2);
    expect(url1).toBe("http://localhost:8080/test/search?id=123&limit=10&name=test");
  });

  test("null result response returns null (Empty type)", async () => {
    const mockFetch = mock(async () => mockResponse(200, { result: null }));
    global.fetch = mockFetch as any;

    const client = createClient(mockRegistry, { baseUrl: "http://localhost:8080" });
    const result = await client.Test.Delete({ id: "123" });

    expect(result).toBeNull();
  });

  test("transport error for malformed envelope without result or error field", async () => {
    const mockFetch = mock(async () => mockResponse(200, { foo: "bar" }));
    global.fetch = mockFetch as any;

    const client = createClient(mockRegistry, { baseUrl: "http://localhost:8080" });

    try {
      await client.Test.Get({ id: "123" });
      expect.unreachable("Should have thrown an error");
    } catch (e: any) {
      expect(e).toBeInstanceOf(TransportError);
      expect(e.kind).toBe("transport");
      expect(e.httpStatus).toBe(200);
      expect(e.message).toBe("Invalid response format: missing result or error field");
    }
  });

  test("transport error includes truncated rawBody for debugging", async () => {
    const longBody = "x".repeat(2000);
    const mockFetch = mock(async () => ({
      ok: false,
      status: 500,
      statusText: "Internal Server Error",
      clone: function() { return this; },
      text: async () => longBody,
    }));
    global.fetch = mockFetch as any;

    const client = createClient(mockRegistry, { baseUrl: "http://localhost:8080" });

    try {
      await client.Test.Get({ id: "123" });
      expect.unreachable("Should have thrown an error");
    } catch (e: any) {
      expect(e).toBeInstanceOf(TransportError);
      expect(e.rawBody?.length).toBe(1000); // Truncated to 1000 chars
    }
  });
});

describe("Atom primitive", () => {
  const atomMetadata = {
    "Tasks.SyncedList": { path: "/tasks/synced", primitive: "atom" as const },
  };

  type AtomManifest = {
    "Tasks.SyncedList": { req: Record<string, never>; res: string[]; primitive: "atom" };
  };

  const atomRegistry: ServiceRegistry<AtomManifest> = {
    manifest: {} as AtomManifest,
    metadata: atomMetadata,
  };

  test("atom returns object with data and state properties", () => {
    const mockFetch = mock(async () => mockResponse(200, { result: [] }));

    const client = createClient(atomRegistry, {
      baseUrl: "http://localhost:8080",
      fetch: mockFetch,
    });

    const atom = client.Tasks.SyncedList;

    // Check that atom has subscribe/getSnapshot shape
    expect(atom).toBeDefined();
    expect(typeof atom.subscribe).toBe("function");
    expect(typeof atom.getSnapshot).toBe("function");
  });

  test("atom.subscribe immediately emits current state", () => {
    const mockFetch = mock(async () => mockResponse(200, { result: [] }));

    const client = createClient(atomRegistry, {
      baseUrl: "http://localhost:8080",
      fetch: mockFetch,
    });

    const atom = client.Tasks.SyncedList;
    const results: SubscriptionResult<string[]>[] = [];

    const unsubscribe = atom.subscribe((result) => {
      results.push(result);
    });

    // Should immediately receive the initial state (connecting since first subscriber)
    expect(results.length).toBeGreaterThanOrEqual(1);
    // First result should be connecting
    expect(results[0].status).toBe("connecting");
    expect(typeof results[0].statusUpdatedAt).toBe("number");

    unsubscribe();
  });

  test("atom.subscribe returns unsubscribe function", () => {
    const mockFetch = mock(async () => mockResponse(200, { result: [] }));

    const client = createClient(atomRegistry, {
      baseUrl: "http://localhost:8080",
      fetch: mockFetch,
    });

    const atom = client.Tasks.SyncedList;
    const unsubscribe = atom.subscribe(() => {});

    expect(typeof unsubscribe).toBe("function");
    unsubscribe();
  });
});

describe("Stream primitive", () => {
  const streamMetadata = {
    "Tasks.Time": { path: "/tasks/time", primitive: "stream" as const },
  };

  type StreamManifest = {
    "Tasks.Time": { req: Record<string, never>; res: { time: string }; primitive: "stream" };
  };

  const streamRegistry: ServiceRegistry<StreamManifest> = {
    manifest: {} as StreamManifest,
    metadata: streamMetadata,
  };

  test("stream returns object with subscribe/getSnapshot and AsyncIterable", () => {
    const mockFetch = mock(async () => mockResponse(200, { result: { time: "now" } }));

    const client = createClient(streamRegistry, {
      baseUrl: "http://localhost:8080",
      fetch: mockFetch,
    });

    const stream = client.Tasks.Time({});

    // Check that stream has subscribe/getSnapshot shape
    expect(stream).toBeDefined();
    expect(typeof stream.subscribe).toBe("function");
    expect(typeof stream.getSnapshot).toBe("function");
    // And is async iterable
    expect(typeof stream[Symbol.asyncIterator]).toBe("function");
  });

  test("stream.subscribe immediately emits current state", () => {
    const mockFetch = mock(async () => mockResponse(200, { result: { time: "now" } }));

    const client = createClient(streamRegistry, {
      baseUrl: "http://localhost:8080",
      fetch: mockFetch,
    });

    const stream = client.Tasks.Time({});
    const results: SubscriptionResult<{ time: string }>[] = [];

    const unsubscribe = stream.subscribe((result) => {
      results.push(result);
    });

    // Should immediately receive the initial state (connecting since first subscriber)
    expect(results.length).toBeGreaterThanOrEqual(1);
    // First result should be connecting
    expect(results[0].status).toBe("connecting");

    unsubscribe();
  });

  test("stream is async iterable", () => {
    const mockFetch = mock(async () => mockResponse(200, { result: { time: "now" } }));

    const client = createClient(streamRegistry, {
      baseUrl: "http://localhost:8080",
      fetch: mockFetch,
    });

    const stream = client.Tasks.Time({});

    // Check that stream is async iterable
    expect(typeof stream[Symbol.asyncIterator]).toBe("function");
  });
});
