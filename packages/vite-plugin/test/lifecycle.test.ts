import { afterEach, describe, expect, test } from "bun:test";
import { spawn } from "node:child_process";
import { EventEmitter } from "node:events";
import { mkdtempSync, readFileSync, rmSync, writeFileSync } from "node:fs";
import { connect, createServer } from "node:net";
import { tmpdir } from "node:os";
import { dirname, join, resolve } from "node:path";
import { createServer as createViteServer, type Plugin, type ViteDevServer } from "vite";
import { createClient, type ServiceRegistry } from "@tygor/client";
import { tygor } from "../src/index.js";

interface ProcessRecord {
  label: string;
  role: string;
  pid: number;
  port?: number;
}

const fixture = resolve(import.meta.dir, "fixtures/managed-process.mjs");
const tempDirs: string[] = [];

afterEach(() => {
  for (const dir of tempDirs.splice(0)) {
    for (const entry of records(join(dir, "pids.jsonl"))) {
      try {
        process.kill(entry.pid, "SIGKILL");
      } catch {
        // Already stopped by the lifecycle under test.
      }
    }
    rmSync(dir, { recursive: true, force: true });
  }
});

function createFakeServer(): { server: ViteDevServer; httpServer: EventEmitter } {
  const httpServer = new EventEmitter();
  return {
    httpServer,
    server: {
      httpServer,
      middlewares: { use: () => undefined },
      hot: { send: () => undefined },
      transformRequest: async () => null,
    } as unknown as ViteDevServer,
  };
}

async function getFreePort(): Promise<number> {
  return new Promise((resolvePort, reject) => {
    const server = createServer();
    server.once("error", reject);
    server.listen(0, "127.0.0.1", () => {
      const address = server.address();
      if (!address || typeof address === "string") return reject(new Error("No TCP address"));
      server.close(() => resolvePort(address.port));
    });
  });
}

function records(pidFile: string): ProcessRecord[] {
  try {
    return readFileSync(pidFile, "utf8")
      .trim()
      .split("\n")
      .filter(Boolean)
      .map((line) => JSON.parse(line));
  } catch {
    return [];
  }
}

function isAlive(pid: number): boolean {
  try {
    process.kill(pid, 0);
    return true;
  } catch {
    return false;
  }
}

async function isPortOpen(port: number): Promise<boolean> {
  return new Promise((resolveOpen) => {
    const socket = connect(port, "127.0.0.1");
    socket.once("connect", () => {
      socket.destroy();
      resolveOpen(true);
    });
    socket.once("error", () => resolveOpen(false));
  });
}

async function waitFor(predicate: () => boolean, message: string, timeout = 10000): Promise<void> {
  const deadline = Date.now() + timeout;
  while (Date.now() < deadline) {
    if (predicate()) return;
    await Bun.sleep(25);
  }
  throw new Error(`Timed out waiting for ${message}`);
}

async function within<T>(promise: Promise<T>, message: string, timeout = 5000): Promise<T> {
  return Promise.race([
    promise,
    Bun.sleep(timeout).then(() => {
      throw new Error(`Timed out waiting for ${message}`);
    }),
  ]);
}

function createPlugin(
  pidFile: string,
  label: string,
  port: number,
  buildMode: "none" | "slow" | "delayed" = "none",
  startMode: "listener" | "backend-wrapper" | "unready-wrapper" | "stubborn-listener" | "watchdog-stubborn" | "hanging-health" | "streaming-health" = "listener",
  pregenMode: "none" | "delayed" | "rewrite" = "none",
  devtoolsMode: "normal" | "delayed" | "stalled-status" | "unready-stubborn" | "unready-child" = "normal",
  watchdog = true,
): Plugin {
  const command = [process.execPath, fixture];
  const pregenCommand = pregenMode === "none"
    ? undefined
    : [...command, pregenMode === "delayed" ? "pregen-delayed" : "pregen-rewrite", pidFile, label]
      .map(JSON.stringify)
      .join(" ");
  const buildCommand =
    buildMode === "none"
      ? undefined
      : [process.execPath, fixture, buildMode === "slow" ? "build" : "build-delayed", pidFile, label]
        .map(JSON.stringify)
        .join(" ");
  return tygor({
    gen: false,
    tygorCommand: [
      ...command,
      devtoolsMode === "normal"
        ? "devtools-wrapper"
        : devtoolsMode === "delayed"
          ? "devtools-delayed-wrapper"
          : devtoolsMode === "stalled-status"
            ? "devtools-stalled-status-wrapper"
            : devtoolsMode === "unready-stubborn"
              ? "devtools-unready-stubborn-wrapper"
              : "unready-wrapper",
      pidFile,
      label,
    ],
    pregen: pregenCommand,
    build: buildCommand,
    start: (serverPort) => ({
      cmd: [...command, startMode, pidFile, label],
      env: { PORT: String(serverPort) },
    }),
    port,
    proxy: ["/stream"],
    workdir: dirname(pidFile),
    watchdog,
  });
}

async function configure(plugin: Plugin, server: ViteDevServer): Promise<void> {
  if (typeof plugin.config === "function") {
    await plugin.config({}, { command: "serve", mode: "test" });
  }
  const hook = plugin.configureServer;
  if (typeof hook !== "function") throw new Error("configureServer hook missing");
  await hook(server);
}

async function closePlugin(plugin: Plugin, httpServer: EventEmitter, emitHttpClose = true): Promise<void> {
  if (emitHttpClose) httpServer.emit("close");
  const hook = plugin.closeBundle;
  if (typeof hook === "function") await hook.call({} as never);
}

describe("Tygor Vite plugin lifecycle", () => {
  test("normal shutdown terminates devtools descendants and backend", async () => {
    const dir = mkdtempSync(join(tmpdir(), "tygor-lifecycle-"));
    tempDirs.push(dir);
    const pidFile = join(dir, "pids.jsonl");
    const plugin = createPlugin(pidFile, "normal", await getFreePort());
    const sigintListeners = process.listenerCount("SIGINT");
    const sigtermListeners = process.listenerCount("SIGTERM");

    const vite = await createViteServer({
      configFile: false,
      logLevel: "silent",
      optimizeDeps: { noDiscovery: true, include: [] },
      plugins: [plugin],
      root: dir,
      server: { host: "127.0.0.1", port: await getFreePort() },
    });
    await vite.listen();
    await waitFor(() => records(pidFile).filter((entry) => entry.label === "normal").length === 3, "managed processes");
    const managed = records(pidFile);
    expect(managed.map((entry) => entry.role).sort()).toEqual(["backend", "devtools", "devtools-wrapper"]);

    await vite.close();
    await waitFor(() => managed.every((entry) => !isAlive(entry.pid)), "normal shutdown");
    for (const entry of managed) {
      if (entry.port) expect(await isPortOpen(entry.port)).toBe(false);
    }
    expect(process.listenerCount("SIGINT")).toBe(sigintListeners);
    expect(process.listenerCount("SIGTERM")).toBe(sigtermListeners);
  }, 15000);

  test("initial pipeline waits for source watcher readiness", async () => {
    const dir = mkdtempSync(join(tmpdir(), "tygor-watcher-ready-"));
    tempDirs.push(dir);
    const pidFile = join(dir, "pids.jsonl");
    const plugin = createPlugin(pidFile, "watcher-ready", await getFreePort(), "none", "listener", "delayed");
    const vite = createFakeServer();
    const messages: string[] = [];
    const originalLog = console.log;
    console.log = (...args: Parameters<typeof console.log>) => {
      messages.push(args.map(String).join(" "));
      originalLog(...args);
    };

    try {
      await configure(plugin, vite.server);
      const readyIndex = messages.findIndex((message) => message.includes("Source watcher ready"));
      const pipelineIndex = messages.findIndex((message) => message.includes("Running pregen:"));
      expect(readyIndex).toBeGreaterThanOrEqual(0);
      expect(pipelineIndex).toBeGreaterThan(readyIndex);
    } finally {
      console.log = originalLog;
      await closePlugin(plugin, vite.httpServer, false);
    }
  }, 15000);

  test("disposal while the source watcher initializes settles startup", async () => {
    const dir = mkdtempSync(join(tmpdir(), "tygor-watcher-dispose-"));
    tempDirs.push(dir);
    const pidFile = join(dir, "pids.jsonl");
    const plugin = createPlugin(pidFile, "watcher-dispose", await getFreePort());
    const vite = createFakeServer();

    if (typeof plugin.config === "function") {
      await plugin.config({}, { command: "serve", mode: "test" });
    }
    const hook = plugin.configureServer;
    if (typeof hook !== "function") throw new Error("configureServer hook missing");
    const startup = hook(vite.server);
    vite.httpServer.emit("close");

    await within(Promise.resolve(startup), "startup cancellation during watcher initialization");
    await closePlugin(plugin, vite.httpServer, false);
    expect(records(pidFile)).toHaveLength(0);
  }, 15000);

  test("empty and negation-only watch sets do not block initial startup", async () => {
    for (const [label, watchPatterns] of [
      ["empty-watch", []],
      ["negated-watch", ["!**/*.go"]],
      ["normalized-negated-watch", ["./!**/*.go"]],
    ] as const) {
      const dir = mkdtempSync(join(tmpdir(), `tygor-${label}-`));
      tempDirs.push(dir);
      const pidFile = join(dir, "pids.jsonl");
      const command = [process.execPath, fixture];
      const plugin = tygor({
        gen: false,
        tygorCommand: [...command, "devtools-wrapper", pidFile, label],
        start: (serverPort) => ({
          cmd: [...command, "listener", pidFile, label],
          env: { PORT: String(serverPort) },
        }),
        port: await getFreePort(),
        workdir: dir,
        watch: [...watchPatterns],
      });
      const vite = createFakeServer();

      try {
        await within(configure(plugin, vite.server), `startup with ${label}`);
        expect(records(pidFile).filter((entry) => entry.label === label).map((entry) => entry.role).sort())
          .toEqual(["backend", "devtools", "devtools-wrapper"]);
      } finally {
        await closePlugin(plugin, vite.httpServer, false);
      }
    }
  }, 15000);

  test("replacement plugin cancels an in-flight build before starting", async () => {
    const dir = mkdtempSync(join(tmpdir(), "tygor-restart-"));
    tempDirs.push(dir);
    const pidFile = join(dir, "pids.jsonl");
    const port = await getFreePort();
    const oldPlugin = createPlugin(pidFile, "old", port, "slow");
    const oldVite = createFakeServer();
    const oldStartup = configure(oldPlugin, oldVite.server);

    await waitFor(
      () => records(pidFile).some((entry) => entry.label === "old" && entry.role === "build"),
      "in-flight build",
    );
    // Vite 6 initializes the replacement before closing the old server.
    const newPlugin = createPlugin(pidFile, "new", port);
    const newVite = createFakeServer();
    await configure(newPlugin, newVite.server);
    oldVite.httpServer.emit("close");
    await oldStartup;

    const oldProcesses = records(pidFile).filter((entry) => entry.label === "old");
    await waitFor(() => oldProcesses.every((entry) => !isAlive(entry.pid)), "old plugin processes");
    expect(oldProcesses.some((entry) => entry.role === "backend")).toBe(false);
    const newProcesses = records(pidFile).filter((entry) => entry.label === "new");
    expect(newProcesses.map((entry) => entry.role).sort()).toEqual(["backend", "devtools", "devtools-wrapper"]);
    expect(newProcesses.every((entry) => isAlive(entry.pid))).toBe(true);

    await closePlugin(newPlugin, newVite.httpServer);
    await waitFor(() => newProcesses.every((entry) => !isAlive(entry.pid)), "replacement shutdown");
    for (const entry of newProcesses) {
      if (entry.port) expect(await isPortOpen(entry.port)).toBe(false);
    }
  }, 15000);

  test("reload stops a backend after its wrapper exits", async () => {
    const dir = mkdtempSync(join(tmpdir(), "tygor-background-"));
    tempDirs.push(dir);
    const pidFile = join(dir, "pids.jsonl");
    const plugin = createPlugin(pidFile, "background", await getFreePort(), "none", "backend-wrapper");
    const vite = createFakeServer();

    await configure(plugin, vite.server);
    const initial = records(pidFile).filter((entry) => entry.label === "background");
    const oldWrapper = initial.find((entry) => entry.role === "backend-wrapper");
    const oldBackend = initial.find((entry) => entry.role === "backend");
    expect(oldWrapper).toBeDefined();
    expect(oldBackend).toBeDefined();
    await waitFor(() => !isAlive(oldWrapper!.pid), "backend wrapper exit");
    expect(isAlive(oldBackend!.pid)).toBe(true);

    writeFileSync(join(dir, "reload.go"), "package reload\n");
    await waitFor(
      () => records(pidFile).filter((entry) => entry.label === "background" && entry.role === "backend").length === 2,
      "replacement backend",
    );
    await waitFor(() => !isAlive(oldBackend!.pid), "old backend shutdown");

    await closePlugin(plugin, vite.httpServer, false);
    const managed = records(pidFile).filter((entry) => entry.label === "background");
    await waitFor(() => managed.every((entry) => !isAlive(entry.pid)), "replacement backend shutdown");
  }, 15000);

  test("backend swap interrupts SSE so the client reconnects", async () => {
    const dir = mkdtempSync(join(tmpdir(), "tygor-stream-swap-"));
    tempDirs.push(dir);
    const pidFile = join(dir, "pids.jsonl");
    const plugin = createPlugin(pidFile, "stream-swap", await getFreePort());
    let iterator: AsyncIterator<{ pid: number }> | undefined;
    const vite = await createViteServer({
      configFile: false,
      logLevel: "silent",
      optimizeDeps: { noDiscovery: true, include: [] },
      plugins: [plugin],
      root: dir,
      server: { host: "127.0.0.1", port: await getFreePort() },
    });

    try {
      await vite.listen();
      const address = vite.httpServer?.address();
      if (!address || typeof address === "string") throw new Error("Vite did not expose a TCP address");
      type StreamManifest = {
        "Test.Stream": { req: Record<string, never>; res: { pid: number }; primitive: "stream" };
      };
      const registry: ServiceRegistry<StreamManifest> = {
        manifest: {} as StreamManifest,
        metadata: { "Test.Stream": { path: "/stream", primitive: "stream" } },
      };
      const client = createClient(registry, { baseUrl: `http://127.0.0.1:${address.port}` });
      iterator = client.Test.Stream({})[Symbol.asyncIterator]();

      const first = await iterator.next();
      expect(first.done).toBe(false);

      writeFileSync(join(dir, "swap.go"), "package swap\n");
      const second = await within(iterator.next(), "stream reconnection");
      expect(second.done).toBe(false);
      expect(second.value?.pid).not.toBe(first.value?.pid);

      await iterator.return?.();
    } finally {
      await iterator?.return?.();
      await vite.close();
    }
  }, 15000);

  test("failed backend candidates are terminated before retrying", async () => {
    const dir = mkdtempSync(join(tmpdir(), "tygor-candidates-"));
    tempDirs.push(dir);
    const pidFile = join(dir, "pids.jsonl");
    const plugin = createPlugin(pidFile, "candidates", await getFreePort(), "none", "unready-wrapper");
    const vite = createFakeServer();

    try {
      await configure(plugin, vite.server);

      const candidates = records(pidFile).filter((entry) => entry.label === "candidates");
      const children = candidates.filter((entry) => entry.role === "candidate-child");
      expect(children).toHaveLength(5);
      expect(candidates.filter((entry) => entry.role === "candidate-overlap")).toHaveLength(0);
      expect(children.every((entry) => !isAlive(entry.pid))).toBe(true);
    } finally {
      await closePlugin(plugin, vite.httpServer, false);
    }
  }, 15000);

  test("a save during reload schedules one trailing reload", async () => {
    const dir = mkdtempSync(join(tmpdir(), "tygor-reload-coalesce-"));
    tempDirs.push(dir);
    const pidFile = join(dir, "pids.jsonl");
    const plugin = createPlugin(pidFile, "coalesce", await getFreePort(), "delayed");
    const vite = createFakeServer();

    try {
      await configure(plugin, vite.server);
      expect(records(pidFile).filter((entry) => entry.label === "coalesce" && entry.role === "build")).toHaveLength(1);

      writeFileSync(join(dir, "first.go"), "package first\n");
      await waitFor(
        () => records(pidFile).filter((entry) => entry.label === "coalesce" && entry.role === "build").length === 2,
        "first reload build",
      );
      writeFileSync(join(dir, "second.go"), "package second\n");
      await waitFor(
        () => records(pidFile).filter((entry) => entry.label === "coalesce" && entry.role === "build").length === 3,
        "trailing reload build",
        4000,
      );
      await Bun.sleep(1000);
      expect(records(pidFile).filter((entry) => entry.label === "coalesce" && entry.role === "build")).toHaveLength(3);
    } finally {
      await closePlugin(plugin, vite.httpServer, false);
    }
  }, 15000);

  test("a save during initial pregen schedules a trailing pipeline", async () => {
    const dir = mkdtempSync(join(tmpdir(), "tygor-pregen-save-"));
    tempDirs.push(dir);
    const pidFile = join(dir, "pids.jsonl");
    const plugin = createPlugin(pidFile, "pregen-save", await getFreePort(), "none", "listener", "delayed");
    const vite = createFakeServer();

    try {
      const startup = configure(plugin, vite.server);
      await waitFor(
        () => records(pidFile).filter((entry) => entry.label === "pregen-save" && entry.role === "pregen").length === 1,
        "initial pregen",
      );
      await Bun.sleep(100);
      writeFileSync(join(dir, "during-pregen.go"), "package duringpregen\n");
      await startup;

      await waitFor(
        () => records(pidFile).filter((entry) => entry.label === "pregen-save" && entry.role === "pregen").length === 2,
        "trailing pregen",
      );
      await Bun.sleep(1000);
      expect(records(pidFile).filter((entry) => entry.label === "pregen-save" && entry.role === "pregen")).toHaveLength(2);
    } finally {
      await closePlugin(plugin, vite.httpServer, false);
    }
  }, 15000);

  test("pregen rewriting a watched Go file does not create an endless reload loop", async () => {
    const dir = mkdtempSync(join(tmpdir(), "tygor-pregen-rewrite-"));
    tempDirs.push(dir);
    const pidFile = join(dir, "pids.jsonl");
    const plugin = createPlugin(pidFile, "pregen-rewrite", await getFreePort(), "none", "listener", "rewrite");
    const vite = createFakeServer();

    try {
      await configure(plugin, vite.server);
      await waitFor(
        () => records(pidFile).filter((entry) => entry.label === "pregen-rewrite" && entry.role === "pregen").length === 2,
        "one trailing pregen",
      );
      await Bun.sleep(1200);
      expect(records(pidFile).filter((entry) => entry.label === "pregen-rewrite" && entry.role === "pregen")).toHaveLength(2);
    } finally {
      await closePlugin(plugin, vite.httpServer, false);
    }
  }, 15000);

  test("a save during devtools startup cannot start a backend before startup ownership", async () => {
    const dir = mkdtempSync(join(tmpdir(), "tygor-devtools-save-"));
    tempDirs.push(dir);
    const pidFile = join(dir, "pids.jsonl");
    const plugin = createPlugin(
      pidFile,
      "devtools-save",
      await getFreePort(),
      "none",
      "listener",
      "none",
      "delayed",
    );
    const vite = createFakeServer();

    try {
      const startup = configure(plugin, vite.server);
      await waitFor(
        () => records(pidFile).some((entry) => entry.label === "devtools-save" && entry.role === "devtools-wrapper"),
        "delayed devtools wrapper",
      );
      writeFileSync(join(dir, "during-devtools.go"), "package duringdevtools\n");
      await startup;

      await waitFor(() => {
        const backends = records(pidFile).filter(
          (entry) => entry.label === "devtools-save" && entry.role === "backend",
        );
        return backends.length === 2 && backends.filter((entry) => isAlive(entry.pid)).length === 1;
      }, "single backend after trailing reload", 8000);
    } finally {
      await closePlugin(plugin, vite.httpServer, false);
    }
  }, 15000);

  test("devtools startup timeout escalates a TERM-resistant process before backend startup", async () => {
    if (process.platform === "win32") return;

    const dir = mkdtempSync(join(tmpdir(), "tygor-devtools-timeout-"));
    tempDirs.push(dir);
    const pidFile = join(dir, "pids.jsonl");
    const plugin = createPlugin(
      pidFile,
      "devtools-timeout",
      await getFreePort(),
      "none",
      "listener",
      "none",
      "unready-stubborn",
    );
    const vite = createFakeServer();

    try {
      await configure(plugin, vite.server);
      const managed = records(pidFile).filter((entry) => entry.label === "devtools-timeout");
      const failedDevtools = managed.filter(
        (entry) => entry.role === "devtools-wrapper" || entry.role === "devtools-unready",
      );
      expect(failedDevtools).toHaveLength(2);
      expect(failedDevtools.every((entry) => !isAlive(entry.pid))).toBe(true);
      expect(managed.filter((entry) => entry.role === "backend" && isAlive(entry.pid))).toHaveLength(1);
    } finally {
      await closePlugin(plugin, vite.httpServer, false);
    }
  }, 40000);

  test("devtools early exit terminates a surviving descendant before backend startup", async () => {
    if (process.platform === "win32") return;

    const dir = mkdtempSync(join(tmpdir(), "tygor-devtools-exit-"));
    tempDirs.push(dir);
    const pidFile = join(dir, "pids.jsonl");
    const plugin = createPlugin(
      pidFile,
      "devtools-exit",
      await getFreePort(),
      "none",
      "listener",
      "none",
      "unready-child",
    );
    const vite = createFakeServer();

    try {
      await configure(plugin, vite.server);
      const managed = records(pidFile).filter((entry) => entry.label === "devtools-exit");
      const descendant = managed.find((entry) => entry.role === "candidate-child");
      expect(descendant).toBeDefined();
      expect(isAlive(descendant!.pid)).toBe(false);
      expect(managed.filter((entry) => entry.role === "backend" && isAlive(entry.pid))).toHaveLength(1);
    } finally {
      await closePlugin(plugin, vite.httpServer, false);
    }
  }, 15000);

  test("a stalled status update cannot block backend retirement or a trailing reload", async () => {
    const dir = mkdtempSync(join(tmpdir(), "tygor-status-stall-"));
    tempDirs.push(dir);
    const pidFile = join(dir, "pids.jsonl");
    const plugin = createPlugin(
      pidFile,
      "status-stall",
      await getFreePort(),
      "none",
      "stubborn-listener",
      "none",
      "stalled-status",
    );
    const vite = createFakeServer();

    try {
      await configure(plugin, vite.server);
      writeFileSync(join(dir, "first.go"), "package first\n");
      await waitFor(
        () => records(pidFile).some((entry) => entry.label === "status-stall" && entry.role === "status-stalled"),
        "stalled post-swap status update",
      );
      writeFileSync(join(dir, "second.go"), "package second\n");

      await waitFor(() => {
        const backends = records(pidFile).filter(
          (entry) => entry.label === "status-stall" && entry.role === "backend",
        );
        return backends.length === 3 && backends.filter((entry) => isAlive(entry.pid)).length === 1;
      }, "backend retirement and trailing reload", 8000);
    } finally {
      await closePlugin(plugin, vite.httpServer, false);
    }
  }, 15000);

  test("health probes time out and disposal cancels an outstanding probe", async () => {
    const dir = mkdtempSync(join(tmpdir(), "tygor-health-stall-"));
    tempDirs.push(dir);
    const pidFile = join(dir, "pids.jsonl");
    const plugin = createPlugin(
      pidFile,
      "health-stall",
      await getFreePort(),
      "none",
      "hanging-health",
    );
    const vite = createFakeServer();
    const startup = configure(plugin, vite.server);

    await waitFor(
      () => records(pidFile).filter(
        (entry) => entry.label === "health-stall" && entry.role === "health-stalled",
      ).length >= 2,
      "second health probe after the first timed out",
      4000,
    );
    await within(closePlugin(plugin, vite.httpServer, false), "health-probe disposal");
    await within(startup, "startup cancellation after health-probe disposal");

    const managed = records(pidFile).filter((entry) => entry.label === "health-stall");
    await waitFor(() => managed.every((entry) => !isAlive(entry.pid)), "health-stall shutdown");
  }, 10000);

  test("a successful health probe closes an unconsumed streaming body", async () => {
    const dir = mkdtempSync(join(tmpdir(), "tygor-health-body-"));
    tempDirs.push(dir);
    const pidFile = join(dir, "pids.jsonl");
    const plugin = createPlugin(
      pidFile,
      "health-body",
      await getFreePort(),
      "none",
      "streaming-health",
    );
    const vite = createFakeServer();

    try {
      await configure(plugin, vite.server);
      await waitFor(
        () => records(pidFile).some(
          (entry) => entry.label === "health-body" && entry.role === "health-closed",
        ),
        "streaming health response closure",
      );
    } finally {
      await closePlugin(plugin, vite.httpServer, false);
    }
  }, 10000);

  test("a save during initial build cannot overlap startup and reload", async () => {
    const dir = mkdtempSync(join(tmpdir(), "tygor-startup-save-"));
    tempDirs.push(dir);
    const pidFile = join(dir, "pids.jsonl");
    const plugin = createPlugin(pidFile, "startup-save", await getFreePort(), "delayed");
    const vite = createFakeServer();

    try {
      const startup = configure(plugin, vite.server);
      await waitFor(
        () => records(pidFile).filter((entry) => entry.label === "startup-save" && entry.role === "build").length === 1,
        "initial build",
      );
      await Bun.sleep(100);
      writeFileSync(join(dir, "during-build.go"), "package duringbuild\n");
      await startup;

      await waitFor(
        () => records(pidFile).filter((entry) => entry.label === "startup-save" && entry.role === "build").length === 2,
        "trailing startup build",
      );
      expect(records(pidFile).filter((entry) => entry.label === "startup-save" && entry.role === "build-overlap")).toHaveLength(0);
    } finally {
      await closePlugin(plugin, vite.httpServer, false);
    }
  }, 15000);

  test("replacement escalates termination for a TERM-resistant backend", async () => {
    if (process.platform === "win32") return;

    const dir = mkdtempSync(join(tmpdir(), "tygor-stubborn-reload-"));
    tempDirs.push(dir);
    const pidFile = join(dir, "pids.jsonl");
    const plugin = createPlugin(pidFile, "stubborn-reload", await getFreePort(), "none", "stubborn-listener");
    const vite = createFakeServer();

    try {
      await configure(plugin, vite.server);
      const oldBackend = records(pidFile).find(
        (entry) => entry.label === "stubborn-reload" && entry.role === "backend",
      );
      expect(oldBackend).toBeDefined();

      writeFileSync(join(dir, "reload.go"), "package reload\n");
      await waitFor(
        () => records(pidFile).filter((entry) => entry.label === "stubborn-reload" && entry.role === "backend").length === 2,
        "TERM-resistant replacement backend",
      );
      await waitFor(() => !isAlive(oldBackend!.pid), "TERM-resistant old backend termination", 6000);
      if (oldBackend!.port) expect(await isPortOpen(oldBackend!.port)).toBe(false);
    } finally {
      await closePlugin(plugin, vite.httpServer, false);
    }
  }, 20000);

  test("watchdog terminates a TERM-resistant backend before restart", async () => {
    if (process.platform === "win32") return;

    const dir = mkdtempSync(join(tmpdir(), "tygor-stubborn-watchdog-"));
    tempDirs.push(dir);
    const pidFile = join(dir, "pids.jsonl");
    const plugin = createPlugin(pidFile, "stubborn-watchdog", await getFreePort(), "none", "watchdog-stubborn");
    const vite = createFakeServer();

    try {
      await configure(plugin, vite.server);
      const oldBackend = records(pidFile).find(
        (entry) => entry.label === "stubborn-watchdog" && entry.role === "backend",
      );
      expect(oldBackend).toBeDefined();

      await waitFor(
        () => records(pidFile).filter((entry) => entry.label === "stubborn-watchdog" && entry.role === "backend").length === 2,
        "watchdog replacement backend",
        12000,
      );
      expect(isAlive(oldBackend!.pid)).toBe(false);
      expect(records(pidFile).filter((entry) => entry.label === "stubborn-watchdog" && entry.role === "watchdog-overlap")).toHaveLength(0);
    } finally {
      await closePlugin(plugin, vite.httpServer, false);
    }
  }, 20000);

  test("watchdog can be disabled while a backend is paused", async () => {
    const dir = mkdtempSync(join(tmpdir(), "tygor-watchdog-disabled-"));
    tempDirs.push(dir);
    const pidFile = join(dir, "pids.jsonl");
    const plugin = createPlugin(
      pidFile,
      "watchdog-disabled",
      await getFreePort(),
      "none",
      "watchdog-stubborn",
      "none",
      "normal",
      false,
    );
    const vite = createFakeServer();

    try {
      await configure(plugin, vite.server);
      await Bun.sleep(7000);
      expect(records(pidFile).filter((entry) => entry.label === "watchdog-disabled" && entry.role === "backend")).toHaveLength(1);
    } finally {
      await closePlugin(plugin, vite.httpServer, false);
    }
  }, 15000);

  test("SIGINT in a Node host terminates an in-flight process tree", async () => {
    if (process.platform === "win32") return;

    const dir = mkdtempSync(join(tmpdir(), "tygor-signal-"));
    tempDirs.push(dir);
    const pidFile = join(dir, "pids.jsonl");
    const host = resolve(import.meta.dir, "fixtures/signal-host.mjs");
    const child = spawn("node", [host, pidFile, fixture, String(await getFreePort())], {
      stdio: ["ignore", "pipe", "pipe"],
    });

    try {
      await waitFor(
        () => records(pidFile).some((entry) => entry.role === "build"),
        "Node-hosted in-flight build",
      );
      const managed = records(pidFile);
      child.kill("SIGINT");
      const exitCode = await new Promise<number | null>((resolveExit) => child.once("exit", resolveExit));

      expect(exitCode).toBe(130);
      await waitFor(() => managed.every((entry) => !isAlive(entry.pid)), "SIGINT cleanup");
      for (const entry of managed) {
        if (entry.port) expect(await isPortOpen(entry.port)).toBe(false);
      }
    } finally {
      if (child.exitCode === null && child.signalCode === null) child.kill("SIGKILL");
    }
  }, 15000);
});
