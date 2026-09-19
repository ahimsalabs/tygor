import { afterEach, describe, expect, test } from "bun:test";
import { spawn } from "node:child_process";
import { EventEmitter } from "node:events";
import { mkdtempSync, readFileSync, rmSync, writeFileSync } from "node:fs";
import { connect, createServer } from "node:net";
import { tmpdir } from "node:os";
import { dirname, join, resolve } from "node:path";
import { createServer as createViteServer, type Plugin, type ViteDevServer } from "vite";
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

function createPlugin(
  pidFile: string,
  label: string,
  port: number,
  buildMode: "none" | "slow" = "none",
  startMode: "listener" | "backend-wrapper" = "listener",
): Plugin {
  const command = [process.execPath, fixture];
  const buildCommand =
    buildMode === "none"
      ? undefined
      : [process.execPath, fixture, "build", pidFile, label].map(JSON.stringify).join(" ");
  return tygor({
    gen: false,
    tygorCommand: [...command, "devtools-wrapper", pidFile, label],
    build: buildCommand,
    start: (serverPort) => ({
      cmd: [...command, startMode, pidFile, label],
      env: { PORT: String(serverPort) },
    }),
    port,
    workdir: dirname(pidFile),
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
