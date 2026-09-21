import { describe, test, expect, mock, spyOn } from "bun:test";
import {
  createClient,
  TygorError,
  ServerError,
  TransportError,
  ServiceRegistry,
  Stream,
  SubscriptionResult,
  StandardSchema,
  StandardSchemaResult,
} from "./runtime";

// Helper to create a mock response (partial Response for testing)
function mockResponse(status: number, body: any, statusText = ""): Response {
  const bodyStr = typeof body === "string" ? body : JSON.stringify(body);
  return {
    ok: status >= 200 && status < 300,
    status,
    statusText,
    clone: function() { return this; },
    text: async () => bodyStr,
  } as Response;
}

function sseResponse(...envelopes: unknown[]): Response {
  return new Response(
    envelopes.map((envelope) => `data: ${JSON.stringify(envelope)}\n\n`).join(""),
    { headers: { "Content-Type": "text/event-stream" } },
  );
}

function controlledSSEResponse(): { response: Response; fail: (error: Error) => void } {
  let streamController!: ReadableStreamDefaultController<Uint8Array>;
  const response = new Response(new ReadableStream<Uint8Array>({
    start(controller) {
      streamController = controller;
    },
  }), { headers: { "Content-Type": "text/event-stream" } });
  return {
    response,
    fail(error: Error) {
      streamController.error(error);
    },
  };
}

function streamingSSEResponse(): {
  response: Response;
  send: (frame: string) => void;
  fail: (error: Error) => void;
} {
  let streamController!: ReadableStreamDefaultController<Uint8Array>;
  const response = new Response(new ReadableStream<Uint8Array>({
    start(controller) {
      streamController = controller;
    },
  }), { headers: { "Content-Type": "text/event-stream" } });
  return {
    response,
    send(frame: string) {
      streamController.enqueue(new TextEncoder().encode(frame));
    },
    fail(error: Error) {
      streamController.error(error);
    },
  };
}

async function waitFor(predicate: () => boolean, message: string, timeout = 1000): Promise<void> {
  const deadline = Date.now() + timeout;
  while (Date.now() < deadline) {
    if (predicate()) return;
    await Bun.sleep(10);
  }
  throw new Error(`Timed out waiting for ${message}`);
}

function standardSchema(validate: StandardSchema["~standard"]["validate"]): StandardSchema {
  return { "~standard": { version: 1, vendor: "test", validate } };
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
    const error = new ServerError("invalid_argument", "Invalid input", 400, {
      field: "email",
      reason: "invalid format",
    });
    expect(error.code).toBe("invalid_argument");
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
    const url1 = (mockFetch.mock.calls as unknown as string[][])[0][0];
    mockFetch.mockClear();

    await client.Test.Search({ limit: 10, id: "123", name: "test" });
    const url2 = (mockFetch.mock.calls as unknown as string[][])[0][0];

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

describe("LiveValue primitive", () => {
  const liveValueMetadata = {
    "Tasks.SyncedList": { path: "/tasks/synced", primitive: "livevalue" as const },
  };

  type LiveValueManifest = {
    "Tasks.SyncedList": { req: Record<string, never>; res: string[]; primitive: "livevalue" };
  };

  const liveValueRegistry: ServiceRegistry<LiveValueManifest> = {
    manifest: {} as LiveValueManifest,
    metadata: liveValueMetadata,
  };

  test("livevalue returns object with data and state properties", () => {
    const mockFetch = mock(async () => mockResponse(200, { result: [] }));

    const client = createClient(liveValueRegistry, {
      baseUrl: "http://localhost:8080",
      fetch: mockFetch,
    });

    const liveValue = client.Tasks.SyncedList;

    // Check that livevalue has subscribe/getSnapshot shape
    expect(liveValue).toBeDefined();
    expect(typeof liveValue.subscribe).toBe("function");
    expect(typeof liveValue.getSnapshot).toBe("function");
  });

  test("livevalue.subscribe immediately emits current state", () => {
    const mockFetch = mock(async () => mockResponse(200, { result: [] }));

    const client = createClient(liveValueRegistry, {
      baseUrl: "http://localhost:8080",
      fetch: mockFetch,
    });

    const liveValue = client.Tasks.SyncedList;
    const results: SubscriptionResult<string[]>[] = [];

    const unsubscribe = liveValue.subscribe((result) => {
      results.push(result);
    });

    // Should immediately receive the initial state (connecting since first subscriber)
    expect(results.length).toBeGreaterThanOrEqual(1);
    // First result should be connecting
    expect(results[0].status).toBe("connecting");
    expect(typeof results[0].statusUpdatedAt).toBe("number");

    unsubscribe();
  });

  test("livevalue.subscribe returns unsubscribe function", () => {
    const mockFetch = mock(async () => mockResponse(200, { result: [] }));

    const client = createClient(liveValueRegistry, {
      baseUrl: "http://localhost:8080",
      fetch: mockFetch,
    });

    const liveValue = client.Tasks.SyncedList;
    const unsubscribe = liveValue.subscribe(() => {});

    expect(typeof unsubscribe).toBe("function");
    unsubscribe();
  });

  test("livevalue snapshots retain identity until state changes", () => {
    const mockFetch = mock((_url: string, init?: RequestInit) => new Promise<Response>((_resolve, reject) => {
      init?.signal?.addEventListener("abort", () => reject(new DOMException("Aborted", "AbortError")));
    }));
    const client = createClient(liveValueRegistry, { fetch: mockFetch });
    const liveValue = client.Tasks.SyncedList;

    const initial = liveValue.getSnapshot();
    expect(liveValue.getSnapshot()).toBe(initial);

    let notified: SubscriptionResult<string[]> | undefined;
    const unsubscribe = liveValue.subscribe((result) => {
      notified = result;
    });
    expect(liveValue.getSnapshot()).not.toBe(initial);
    expect(liveValue.getSnapshot()).toBe(notified!);

    unsubscribe();
  });

  test("livevalue retries transient fetch failures", async () => {
    let calls = 0;
    let streamController: ReadableStreamDefaultController<Uint8Array>;
    const mockFetch = mock(async () => {
      calls++;
      if (calls === 1) throw new Error("backend unavailable");
      return new Response(new ReadableStream<Uint8Array>({
        start(controller) {
          streamController = controller;
        },
      }), { headers: { "Content-Type": "text/event-stream" } });
    });
    const client = createClient(liveValueRegistry, { fetch: mockFetch });
    const statuses: string[] = [];
    const unsubscribe = client.Tasks.SyncedList.subscribe((result) => statuses.push(result.status));

    await waitFor(() => calls === 2 && client.Tasks.SyncedList.getSnapshot().status === "connected", "livevalue reconnect");
    expect(statuses).toContain("reconnecting");
    expect(statuses).not.toContain("error");

    unsubscribe();
    streamController!.close();
  });

  test("livevalue treats unary setup errors as terminal without reconnecting", async () => {
    let calls = 0;
    const mockFetch = mock(async () => {
      calls++;
      return new Response(
        JSON.stringify({ error: { code: "internal", message: "streaming unsupported" } }),
        { status: 500, headers: { "Content-Type": "application/json" } },
      );
    });
    const liveValue = createClient(liveValueRegistry, { fetch: mockFetch }).Tasks.SyncedList;
    const unsubscribe = liveValue.subscribe(() => {});

    await waitFor(() => liveValue.getSnapshot().status === "error", "terminal setup error");
    await Bun.sleep(150);
    expect(calls).toBe(1);
    expect(liveValue.getSnapshot().error).toBeInstanceOf(ServerError);

    unsubscribe();
  });

  test("livevalue terminal errors last for one observed connection session", async () => {
    let calls = 0;
    const mockFetch = mock(async () => {
      calls++;
      if (calls === 1) {
        return sseResponse(
          { result: ["cached"] },
          { error: { code: "internal", message: "session failed" } },
        );
      }
      return new Response(new ReadableStream<Uint8Array>({
        start(controller) {
          controller.enqueue(new TextEncoder().encode(`data: {"result":["recovered"]}\n\n`));
        },
      }), { headers: { "Content-Type": "text/event-stream" } });
    });
    const client = createClient(liveValueRegistry, { fetch: mockFetch });
    const liveValue = client.Tasks.SyncedList;
    const unsubscribeFirst = liveValue.subscribe(() => {});

    await waitFor(() => liveValue.getSnapshot().status === "error", "terminal livevalue error");
    expect(liveValue.getSnapshot().data).toEqual(["cached"]);

    const unsubscribeSecond = liveValue.subscribe(() => {});
    expect(calls).toBe(1);
    unsubscribeFirst();
    expect(liveValue.getSnapshot().status).toBe("error");

    unsubscribeSecond();
    expect(liveValue.getSnapshot()).toMatchObject({
      status: "disconnected",
      error: undefined,
      data: ["cached"],
    });

    const unsubscribeRecovered = liveValue.subscribe(() => {});
    await waitFor(() => liveValue.getSnapshot().data?.[0] === "recovered", "livevalue recovery");
    expect(calls).toBe(2);
    unsubscribeRecovered();
  });

  test("livevalue ignores buffered errors after its last subscriber leaves", async () => {
    let calls = 0;
    let replacementController!: ReadableStreamDefaultController<Uint8Array>;
    const mockFetch = mock(async () => {
      calls++;
      if (calls === 1) {
        return sseResponse(
          { result: ["first"] },
          { error: { code: "internal", message: "stale failure" } },
        );
      }
      return new Response(new ReadableStream<Uint8Array>({
        start(controller) {
          replacementController = controller;
          controller.enqueue(new TextEncoder().encode(`data: {"result":["recovered"]}\n\n`));
        },
      }), { headers: { "Content-Type": "text/event-stream" } });
    });
    const liveValue = createClient(liveValueRegistry, { fetch: mockFetch }).Tasks.SyncedList;
    let unsubscribe: () => void = () => undefined;
    unsubscribe = liveValue.subscribe((result) => {
      if (result.data?.[0] === "first") unsubscribe();
    });

    await waitFor(() => liveValue.getSnapshot().status === "disconnected", "livevalue unsubscribe");
    expect(liveValue.getSnapshot().error).toBeUndefined();

    const unsubscribeRecovered = liveValue.subscribe(() => {});
    await waitFor(() => liveValue.getSnapshot().data?.[0] === "recovered", "livevalue resubscription");
    expect(calls).toBe(2);
    unsubscribeRecovered();
    replacementController.close();
  });

  test("livevalue does not reconnect after all subscribers leave during abort cleanup", async () => {
    const firstConnection = controlledSSEResponse();
    let calls = 0;
    const mockFetch = mock(async () => {
      calls++;
      return calls === 1 ? firstConnection.response : sseResponse({ result: ["orphaned"] });
    });
    const client = createClient(liveValueRegistry, { fetch: mockFetch });
    const liveValue = client.Tasks.SyncedList;

    const unsubscribeFirst = liveValue.subscribe(() => {});
    await waitFor(() => liveValue.getSnapshot().status === "connected", "first livevalue connection");
    unsubscribeFirst();

    const unsubscribeReplacement = liveValue.subscribe(() => {});
    unsubscribeReplacement();
    firstConnection.fail(new Error("aborted connection cleanup"));
    await Bun.sleep(20);

    expect(calls).toBe(1);
    expect(liveValue.getSnapshot().status).toBe("disconnected");
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

  test("stream snapshots retain identity until state changes", () => {
    const mockFetch = mock((_url: string, init?: RequestInit) => new Promise<Response>((_resolve, reject) => {
      init?.signal?.addEventListener("abort", () => reject(new DOMException("Aborted", "AbortError")));
    }));
    const client = createClient(streamRegistry, { fetch: mockFetch });
    const stream = client.Tasks.Time({});

    const initial = stream.getSnapshot();
    expect(stream.getSnapshot()).toBe(initial);

    let notified: SubscriptionResult<{ time: string }> | undefined;
    const unsubscribe = stream.subscribe((result) => {
      notified = result;
    });
    expect(stream.getSnapshot()).not.toBe(initial);
    expect(stream.getSnapshot()).toBe(notified!);

    unsubscribe();
  });

  test("replacement iterator survives aborted connection cleanup", async () => {
    const firstConnection = controlledSSEResponse();
    let calls = 0;
    const mockFetch = mock(async () => {
      calls++;
      if (calls === 1) return firstConnection.response;
      return sseResponse({ result: { time: "replacement" } });
    });
    const client = createClient(streamRegistry, { fetch: mockFetch });
    const stream = client.Tasks.Time({});
    const firstIterator = stream[Symbol.asyncIterator]();
    const firstRead = firstIterator.next();

    await waitFor(() => stream.getSnapshot().status === "connected", "first stream connection");
    expect(await firstIterator.return?.()).toEqual({ done: true, value: undefined });
    expect(await firstRead).toEqual({ done: true, value: undefined });

    const replacement = stream[Symbol.asyncIterator]();
    const replacementRead = replacement.next();
    firstConnection.fail(new Error("aborted connection cleanup"));

    expect(await replacementRead).toEqual({ done: false, value: { time: "replacement" } });
    expect(calls).toBe(2);
    expect(await replacement.next()).toEqual({ done: true, value: undefined });
  });

  test("stream ignores buffered errors after cancellation", async () => {
    const mockFetch = mock(async () => sseResponse(
      { result: { time: "first" } },
      { error: { code: "internal", message: "stale failure" } },
    ));
    const stream = createClient(streamRegistry, { fetch: mockFetch }).Tasks.Time({});
    let unsubscribe: () => void = () => undefined;
    unsubscribe = stream.subscribe((result) => {
      if (result.data?.time === "first") unsubscribe();
    });

    await waitFor(() => stream.getSnapshot().status === "disconnected", "stream cancellation");
    expect(stream.getSnapshot().error).toBeUndefined();
    expect(mockFetch).toHaveBeenCalledTimes(1);
  });

  test("async iterator drains events and completes on finite EOF", async () => {
    const mockFetch = mock(async () => sseResponse(
      { result: { time: "first" } },
      { result: { time: "second" } },
    ));
    const client = createClient(streamRegistry, { fetch: mockFetch });
    const stream = client.Tasks.Time({});
    const iterator = stream[Symbol.asyncIterator]();

    const first = iterator.next();
    const second = iterator.next();
    const completed = iterator.next();

    expect(await first).toEqual({ done: false, value: { time: "first" } });
    expect(await second).toEqual({ done: false, value: { time: "second" } });
    expect(await completed).toEqual({ done: true, value: undefined });
    expect(await iterator.next()).toEqual({ done: true, value: undefined });
    expect(await stream[Symbol.asyncIterator]().next()).toEqual({ done: true, value: undefined });
    expect(mockFetch).toHaveBeenCalledTimes(1);
  });

  test("async iterator rejects pending and future reads on terminal SSE errors", async () => {
    const mockFetch = mock(async () => sseResponse({
      error: { code: "internal", message: "stream failed" },
    }));
    const client = createClient(streamRegistry, { fetch: mockFetch });
    const stream = client.Tasks.Time({});
    const iterator = stream[Symbol.asyncIterator]();

    await expect(iterator.next()).rejects.toBeInstanceOf(ServerError);
    await expect(iterator.next()).rejects.toThrow("stream failed");
    await expect(stream[Symbol.asyncIterator]().next()).rejects.toThrow("stream failed");
    expect(mockFetch).toHaveBeenCalledTimes(1);
  });

  test("async iterator treats thrown request validation as terminal", async () => {
    const validationFailure = new Error("request validator crashed");
    let validationCalls = 0;
    const mockFetch = mock(async () => sseResponse({ result: { time: "unexpected" } }));
    const client = createClient(streamRegistry, {
      fetch: mockFetch,
      schemas: {
        "Tasks.Time": {
          request: standardSchema(async () => {
            validationCalls++;
            throw validationFailure;
          }),
          response: standardSchema((value) => ({ value })),
        },
      },
    });
    const iterator = client.Tasks.Time({})[Symbol.asyncIterator]();

    await expect(iterator.next()).rejects.toBe(validationFailure);
    await expect(iterator.next()).rejects.toBe(validationFailure);
    await Bun.sleep(150);
    expect(validationCalls).toBe(1);
    expect(mockFetch).not.toHaveBeenCalled();
  });

  test("abort remains terminal when deferred validation finishes", async () => {
    let resolveValidation!: (result: StandardSchemaResult<unknown>) => void;
    const validation = new Promise<StandardSchemaResult<unknown>>((resolve) => {
      resolveValidation = resolve;
    });
    const abortController = new AbortController();
    const mockFetch = mock(async () => sseResponse({ result: { time: "unexpected" } }));
    const client = createClient(streamRegistry, {
      fetch: mockFetch,
      schemas: {
        "Tasks.Time": {
          request: standardSchema(() => validation),
          response: standardSchema((value) => ({ value })),
        },
      },
    });
    const stream = client.Tasks.Time({}, { signal: abortController.signal });
    const iterator = stream[Symbol.asyncIterator]();
    const pending = iterator.next();

    abortController.abort();
    expect(await pending).toEqual({ done: true, value: undefined });
    resolveValidation({ issues: [{ message: "too late" }] });
    await Bun.sleep(0);

    expect(stream.getSnapshot().status).toBe("disconnected");
    expect(await stream[Symbol.asyncIterator]().next()).toEqual({ done: true, value: undefined });
    expect(mockFetch).not.toHaveBeenCalled();
  });

  test("iterator return discards values buffered before EOF", async () => {
    const mockFetch = mock(async () => sseResponse({ result: { time: "buffered" } }));
    const client = createClient(streamRegistry, { fetch: mockFetch });
    const stream = client.Tasks.Time({});
    const iterator = stream[Symbol.asyncIterator]();

    await waitFor(() => stream.getSnapshot().status === "completed", "stream completion");
    expect(await iterator.return?.()).toEqual({ done: true, value: undefined });
    expect(await iterator.next()).toEqual({ done: true, value: undefined });
  });

  test("async iterator survives transient fetch failures and resumes", async () => {
    let calls = 0;
    const mockFetch = mock(async () => {
      calls++;
      if (calls === 1) throw new Error("backend unavailable");
      return sseResponse({ result: { time: "recovered" } });
    });
    const client = createClient(streamRegistry, { fetch: mockFetch });
    const stream = client.Tasks.Time({});
    const statuses: string[] = [];
    const unsubscribe = stream.subscribe((result) => statuses.push(result.status));
    const iterator = stream[Symbol.asyncIterator]();

    expect(await iterator.next()).toEqual({ done: false, value: { time: "recovered" } });
    expect(await iterator.next()).toEqual({ done: true, value: undefined });
    expect(calls).toBe(2);
    expect(statuses).toContain("reconnecting");
    expect(statuses).not.toContain("error");

    unsubscribe();
  });

  test("reconnect sends the last SSE event ID before resuming the same iterator", async () => {
    const firstConnection = streamingSSEResponse();
    let calls = 0;
    const mockFetch = mock(async (_url: string, init?: RequestInit) => {
      calls++;
      if (calls === 1) return firstConnection.response;
      expect(new Headers(init?.headers).get("Last-Event-ID")).toBe("41");
      return new Response("id: 42\ndata: {\"result\":{\"time\":\"second\"}}\n\n", {
        headers: { "Content-Type": "text/event-stream" },
      });
    });
    const stream = createClient(streamRegistry, { fetch: mockFetch }).Tasks.Time({});
    const iterator = stream[Symbol.asyncIterator]();

    firstConnection.send("id: 41\ndata: {\"result\":{\"time\":\"first\"}}\n\n");
    expect(await iterator.next()).toEqual({ done: false, value: { time: "first" } });
    firstConnection.fail(new Error("connection reset"));

    expect(await iterator.next()).toEqual({ done: false, value: { time: "second" } });
    expect(await iterator.next()).toEqual({ done: true, value: undefined });
    expect(calls).toBe(2);
  });

  test("connection failures become terminal after bounded retries", async () => {
    const mockFetch = mock(async () => {
      throw new TypeError("connection refused");
    });
    const stream = createClient(streamRegistry, { fetch: mockFetch }).Tasks.Time({});
    const iterator = stream[Symbol.asyncIterator]();

    await expect(iterator.next()).rejects.toMatchObject({
      name: "TransportError",
      message: "connection refused",
    });
    expect(stream.getSnapshot().status).toBe("error");
    expect(mockFetch).toHaveBeenCalledTimes(4);
  });

  test("retries transient gateway responses", async () => {
    let calls = 0;
    const mockFetch = mock(async () => {
      calls++;
      if (calls === 1) {
        return new Response("bad gateway", { status: 502, statusText: "Bad Gateway" });
      }
      return sseResponse({ result: { time: "recovered" } });
    });
    const stream = createClient(streamRegistry, { fetch: mockFetch }).Tasks.Time({});
    const iterator = stream[Symbol.asyncIterator]();

    expect(await iterator.next()).toEqual({ done: false, value: { time: "recovered" } });
    expect(calls).toBe(2);
  });

  test("aborting a stream completes pending iterator reads without retrying", async () => {
    let calls = 0;
    const abortController = new AbortController();
    const mockFetch = mock((_url: string, init?: RequestInit) => {
      calls++;
      return new Promise<Response>((_resolve, reject) => {
        init?.signal?.addEventListener("abort", () => reject(new DOMException("Aborted", "AbortError")));
      });
    });
    const client = createClient(streamRegistry, { fetch: mockFetch });
    const iterator = client.Tasks.Time({}, { signal: abortController.signal })[Symbol.asyncIterator]();
    const pending = iterator.next();

    abortController.abort();

    expect(await pending).toEqual({ done: true, value: undefined });
    expect(await iterator.next()).toEqual({ done: true, value: undefined });
    await Bun.sleep(150);
    expect(calls).toBe(1);
  });

  test("explicit cancellation releases and restores the external abort listener", async () => {
    const abortController = new AbortController();
    const addEventListener = spyOn(abortController.signal, "addEventListener");
    const removeEventListener = spyOn(abortController.signal, "removeEventListener");
    const firstConnection = controlledSSEResponse();
    const secondConnection = controlledSSEResponse();
    let calls = 0;
    const mockFetch = mock(async () => ++calls === 1 ? firstConnection.response : secondConnection.response);
    const stream = createClient(streamRegistry, { fetch: mockFetch })
      .Tasks.Time({}, { signal: abortController.signal });

    const first = stream[Symbol.asyncIterator]();
    await waitFor(() => stream.getSnapshot().status === "connected", "first stream connection");
    expect(addEventListener).toHaveBeenCalledTimes(1);

    expect(await first.return?.()).toEqual({ done: true, value: undefined });
    expect(removeEventListener).toHaveBeenCalledTimes(1);
    firstConnection.fail(new Error("cancelled first connection"));

    const replacement = stream[Symbol.asyncIterator]();
    await waitFor(() => calls === 2, "replacement stream connection");
    expect(addEventListener).toHaveBeenCalledTimes(2);

    const pending = replacement.next();
    abortController.abort();
    expect(await pending).toEqual({ done: true, value: undefined });
    expect(removeEventListener).toHaveBeenCalledTimes(2);
    expect(stream.getSnapshot().status).toBe("disconnected");
    secondConnection.fail(new Error("cancelled replacement connection"));
  });

  test("external abort settles a replacement waiting for aborted connection cleanup", async () => {
    const abortController = new AbortController();
    const firstConnection = controlledSSEResponse();
    let calls = 0;
    const mockFetch = mock(async () => {
      calls++;
      return firstConnection.response;
    });
    const stream = createClient(streamRegistry, { fetch: mockFetch })
      .Tasks.Time({}, { signal: abortController.signal });
    const first = stream[Symbol.asyncIterator]();

    await waitFor(() => stream.getSnapshot().status === "connected", "first stream connection");
    expect(await first.return?.()).toEqual({ done: true, value: undefined });

    const replacement = stream[Symbol.asyncIterator]();
    const pending = replacement.next();
    abortController.abort();
    const settled = await Promise.race([
      pending,
      Bun.sleep(100).then(() => "timed out" as const),
    ]);
    firstConnection.fail(new Error("cancelled first connection"));

    expect(settled).toEqual({ done: true, value: undefined });
    expect(await replacement.next()).toEqual({ done: true, value: undefined });
    expect(calls).toBe(1);
  });

  test("an abort while no consumer is attached cancels the next iterator", async () => {
    const abortController = new AbortController();
    const mockFetch = mock(async () => sseResponse({ result: { time: "unexpected" } }));
    const stream = createClient(streamRegistry, { fetch: mockFetch })
      .Tasks.Time({}, { signal: abortController.signal });

    abortController.abort();
    const iterator = stream[Symbol.asyncIterator]();

    expect(await iterator.next()).toEqual({ done: true, value: undefined });
    expect(await iterator.next()).toEqual({ done: true, value: undefined });
    expect(mockFetch).not.toHaveBeenCalled();
  });

  test("an abort between consumers cancels the replacement iterator", async () => {
    const abortController = new AbortController();
    const firstConnection = controlledSSEResponse();
    const mockFetch = mock(async () => firstConnection.response);
    const stream = createClient(streamRegistry, { fetch: mockFetch })
      .Tasks.Time({}, { signal: abortController.signal });
    const first = stream[Symbol.asyncIterator]();

    await waitFor(() => stream.getSnapshot().status === "connected", "first stream connection");
    expect(await first.return?.()).toEqual({ done: true, value: undefined });
    abortController.abort();

    const replacement = stream[Symbol.asyncIterator]();
    expect(await replacement.next()).toEqual({ done: true, value: undefined });
    expect(mockFetch).toHaveBeenCalledTimes(1);
    firstConnection.fail(new Error("cancelled first connection"));
  });

  test("late terminal consumers do not reattach the external abort listener", async () => {
    const abortController = new AbortController();
    const addEventListener = spyOn(abortController.signal, "addEventListener");
    const removeEventListener = spyOn(abortController.signal, "removeEventListener");
    const stream = createClient(streamRegistry, {
      fetch: mock(async () => sseResponse({ result: { time: "done" } })),
    }).Tasks.Time({}, { signal: abortController.signal });
    const first = stream[Symbol.asyncIterator]();

    expect(await first.next()).toEqual({ done: false, value: { time: "done" } });
    expect(await first.next()).toEqual({ done: true, value: undefined });
    expect(addEventListener).toHaveBeenCalledTimes(1);
    expect(removeEventListener).toHaveBeenCalledTimes(1);

    const late = stream[Symbol.asyncIterator]();
    expect(await late.next()).toEqual({ done: true, value: undefined });
    const unsubscribe = stream.subscribe(() => undefined);
    unsubscribe();

    expect(addEventListener).toHaveBeenCalledTimes(1);
    expect(removeEventListener).toHaveBeenCalledTimes(1);
  });

  test("late failed consumers do not reattach the external abort listener", async () => {
    const abortController = new AbortController();
    const addEventListener = spyOn(abortController.signal, "addEventListener");
    const removeEventListener = spyOn(abortController.signal, "removeEventListener");
    const stream = createClient(streamRegistry, {
      fetch: mock(async () => sseResponse({ error: { code: "internal", message: "failed" } })),
    }).Tasks.Time({}, { signal: abortController.signal });
    const first = stream[Symbol.asyncIterator]();

    await expect(first.next()).rejects.toBeInstanceOf(ServerError);
    expect(addEventListener).toHaveBeenCalledTimes(1);
    expect(removeEventListener).toHaveBeenCalledTimes(1);

    const late = stream[Symbol.asyncIterator]();
    await expect(late.next()).rejects.toBeInstanceOf(ServerError);
    const unsubscribe = stream.subscribe(() => undefined);
    unsubscribe();

    expect(addEventListener).toHaveBeenCalledTimes(1);
    expect(removeEventListener).toHaveBeenCalledTimes(1);
  });
});
