import { spawn, spawnSync, ChildProcess, type SpawnOptions } from "node:child_process";
import { resolve, dirname } from "node:path";
import { mkdirSync, readFileSync, existsSync } from "node:fs";
import { createServer } from "node:net";
import { request as httpRequest, ClientRequest } from "node:http";
import { watch } from "chokidar";
import type { Plugin, ViteDevServer } from "vite";
import type { IncomingMessage, ServerResponse } from "node:http";
import pc from "picocolors";
import { createClient } from "@tygor/client";
import { registry as devserverRegistry } from "./devserver/manifest.js";
import { clientBundle } from "./generated/client-bundle.js";
import { VERSION } from "./generated/version.js";

export interface TygorDevOptions {
  /** Run tygor gen automatically (default: true). Set to false to disable. */
  gen?: boolean;
  /** Command to run before tygor gen (e.g., sqlc generate). Use for codegen that produces Go code tygor needs to parse. */
  pregen?: string | string[];
  /** Command to run after tygor gen but before build (e.g., custom codegen) */
  prebuild?: string | string[];
  /** Build command (e.g., "go build -o ./tmp/server ."). If provided, build errors are distinguished from runtime errors */
  build?: string | string[];
  /** Path to build output file - parent directory will be created automatically */
  buildOutput?: string;
  /** Function that returns the command to start the server */
  start: (port: number) => {
    cmd: string | string[];
    env?: Record<string, string>;
    cwd?: string;
  };
  /** Directory containing generated RPC files (default: './src/rpc'). Used for serving discovery.json, deriving proxy paths, and tygor gen output */
  rpcDir?: string;
  /** Glob patterns to watch (default: ['**\/*.go']) */
  watch?: string[];
  /** Glob patterns to ignore (default: ['node_modules', '.git', 'tmp']) */
  ignore?: string[];
  /** Health check endpoint - set to false to use TCP probe (default: false) */
  health?: string | false;
  /** Starting port to search from (default: 8080) */
  port?: number;
  /** Working directory for Go commands and file watcher (default: process.cwd()) */
  workdir?: string;
  /** Proxy path prefixes to route to Go server (auto-derived from discovery.json if not specified) */
  proxy?: string[];
  /** Override the tygor command (default: auto-detected local or 'go run tygor.dev/cmd/tygor@v{version}') */
  tygorCommand?: string | string[];
  /**
   * Prefix for proxied API paths (default: '/').
   * Requests matching this prefix are forwarded to the Go server.
   * Example: '/api' proxies /api/ServiceName/* to the Go server as /ServiceName/*
   *
   * Set this to match your production API mount point, then use the same
   * value for baseUrl in createClient().
   */
  proxyPrefix?: string;
}

interface ServerState {
  process: ChildProcess | null;
  port: number;
  ready: boolean;
}

interface DevServerState {
  process: ChildProcess | null;
  port: number;
  ready: boolean;
}

const DEFAULT_OPTIONS = {
  gen: true,
  watch: ["**/*.go"],
  ignore: ["**/node_modules", "**/.git", "**/.tygor", "**/dist"],
  health: false as const,
  port: 8080,
  rpcDir: "./src/rpc",
  proxyPrefix: "/",
};

interface PluginLifecycle {
  dispose(): Promise<void>;
  disposeSync(): void;
}

const activeLifecycles = new Map<string, PluginLifecycle>();
let handlingSignal = false;

const onSignal = (signal: NodeJS.Signals) => {
  if (handlingSignal) return;
  handlingSignal = true;
  void Promise.allSettled([...activeLifecycles.values()].map((lifecycle) => lifecycle.dispose())).then(() => {
    process.exitCode = signal === "SIGINT" ? 130 : 143;
    process.exit();
  });
};

const onExit = () => {
  for (const lifecycle of activeLifecycles.values()) lifecycle.disposeSync();
};

function registerLifecycle(key: string, lifecycle: PluginLifecycle): () => void {
  if (activeLifecycles.size === 0) {
    process.on("SIGINT", onSignal);
    process.on("SIGTERM", onSignal);
    process.on("exit", onExit);
  }
  activeLifecycles.set(key, lifecycle);

  return () => {
    if (activeLifecycles.get(key) === lifecycle) activeLifecycles.delete(key);
    if (activeLifecycles.size === 0) {
      process.off("SIGINT", onSignal);
      process.off("SIGTERM", onSignal);
      process.off("exit", onExit);
      handlingSignal = false;
    }
  };
}

/** Find an available port starting from the given port */
async function findPort(startPort: number): Promise<number> {
  return new Promise((resolve) => {
    const server = createServer();
    server.listen(startPort, () => {
      server.close(() => resolve(startPort));
    });
    server.on("error", () => {
      resolve(findPort(startPort + 1));
    });
  });
}

/** Get the tygor command - uses option, local during development, or go run for published */
function getTygorCommand(override?: string | string[]): string[] {
  // Use explicit override if provided
  if (override) {
    return Array.isArray(override) ? override : override.split(" ");
  }
  // Check if we're in the tygor repo (development mode)
  // Walk up from cwd looking for cmd/tygor (handles examples/ subdirs)
  let dir = process.cwd();
  for (let i = 0; i < 5; i++) {
    const tygorCmd = resolve(dir, "cmd/tygor");
    if (existsSync(tygorCmd)) {
      return ["go", "run", tygorCmd];
    }
    const parent = dirname(dir);
    if (parent === dir) break; // reached root
    dir = parent;
  }
  // Published mode - use go run with pinned version
  return ["go", "run", `tygor.dev/cmd/tygor@v${VERSION}`];
}

/** Auto-detect devtools bundle path when in tygor repo */
function getDevtoolsBundlePath(): string | undefined {
  // Walk up from cwd looking for vite-plugin/src/generated/client-bundle.ts
  let dir = process.cwd();
  for (let i = 0; i < 5; i++) {
    const bundlePath = resolve(dir, "vite-plugin/src/generated/client-bundle.ts");
    if (existsSync(bundlePath)) {
      return bundlePath;
    }
    const parent = dirname(dir);
    if (parent === dir) break;
    dir = parent;
  }
  return undefined;
}

export function tygor(options: TygorDevOptions): Plugin {
  const opts = { ...DEFAULT_OPTIONS, ...options };
  // Auto-detect devtools bundle path when in tygor repo (for hot reload during development)
  const devtoolsBundlePath = getDevtoolsBundlePath();
  const workdir = resolve(process.cwd(), opts.workdir ?? ".");
  const lifecycleKey = `${workdir}\0${opts.port}`;

  let currentServer: ServerState = { process: null, port: opts.port, ready: false };
  let nextServer: ServerState | null = null;
  let devServer: DevServerState = { process: null, port: 9000, ready: false };
  let buildError: string | null = null;
  let errorPhase: "pregen" | "gen" | "prebuild" | "build" | "runtime" | null = null;
  let errorCommand: string | null = null;
  let errorExitCode: number | null = null;
  let currentPhase: "idle" | "prebuild" | "building" | "starting" = "idle";
  let isReloading = false;
  let isDev = false;
  let ignoreFileChanges = false; // Ignore file changes during pregen/gen to prevent loops
  let disposed = false;
  let dispose: (() => Promise<void>) | null = null;
  const managedProcesses = new Map<number, ChildProcess>();
  const lifecycleTimers = new Set<ReturnType<typeof setTimeout>>();
  const pendingCancellations = new Set<() => void>();

  const log = (msg: string) => console.log(pc.cyan("[tygor]"), msg);
  const logError = (msg: string) => console.log(pc.cyan("[tygor]"), pc.red(msg));

  function schedule(callback: () => void, delay: number): ReturnType<typeof setTimeout> {
    const timer = setTimeout(() => {
      lifecycleTimers.delete(timer);
      callback();
    }, delay);
    lifecycleTimers.add(timer);
    return timer;
  }

  function lifecycleDelay(delay: number): Promise<boolean> {
    return new Promise((resolveDelay) => {
      let timer: ReturnType<typeof setTimeout>;
      const cancel = () => {
        clearTimeout(timer);
        lifecycleTimers.delete(timer);
        pendingCancellations.delete(cancel);
        resolveDelay(false);
      };
      timer = schedule(() => {
        pendingCancellations.delete(cancel);
        resolveDelay(true);
      }, delay);
      pendingCancellations.add(cancel);
    });
  }

  function trackProcess(proc: ChildProcess): ChildProcess {
    if (!proc.pid) return proc;
    const processGroup = proc.pid;
    managedProcesses.set(processGroup, proc);
    proc.once("close", () => {
      if (!processTreeIsRunning(proc)) managedProcesses.delete(processGroup);
    });
    return proc;
  }

  function spawnManaged(command: string, args: string[], options: SpawnOptions): ChildProcess {
    const proc = trackProcess(
      spawn(command, args, {
        ...options,
        // Production build commands remain in the foreground process group so
        // normal terminal signals reach them without dev-server lifecycle hooks.
        detached: isDev && process.platform !== "win32",
      }),
    );
    if (disposed) signalProcessTree(proc, "SIGTERM");
    return proc;
  }

  function execManaged(command: string): Promise<{ error: Error | null; stdout: string; stderr: string }> {
    const proc = spawnManaged(command, [], {
      cwd: workdir,
      shell: true,
      stdio: ["ignore", "pipe", "pipe"],
    });
    return new Promise((resolveCommand) => {
      let stdout = "";
      let stderr = "";
      let completed = false;
      proc.stdout?.on("data", (data) => (stdout += data.toString()));
      proc.stderr?.on("data", (data) => (stderr += data.toString()));
      proc.once("error", (error) => {
        completed = true;
        resolveCommand({ error, stdout, stderr });
      });
      proc.once("close", (code) => {
        if (completed) return;
        completed = true;
        const error = code === 0 ? null : new Error(`Command exited with code ${code}`);
        resolveCommand({ error, stdout, stderr });
      });
    });
  }

  function signalProcessTree(proc: ChildProcess, signal: NodeJS.Signals): void {
    if (!proc.pid) return;

    if (process.platform === "win32") {
      const args = ["/pid", String(proc.pid), "/t"];
      if (signal === "SIGKILL") args.push("/f");
      spawn("taskkill", args, { stdio: "ignore" }).unref();
      return;
    }

    try {
      process.kill(-proc.pid, signal);
    } catch (error) {
      if ((error as NodeJS.ErrnoException).code === "ERR_OUT_OF_RANGE") {
        spawn("kill", [`-${signal}`, "--", `-${proc.pid}`], { stdio: "ignore" }).unref();
        return;
      }
      if ((error as NodeJS.ErrnoException).code !== "ESRCH") {
        logError(`Failed to stop process group ${proc.pid}: ${(error as Error).message}`);
      }
    }
  }

  function killProcessTreeSync(proc: ChildProcess): void {
    if (!proc.pid) return;
    if (process.platform === "win32") {
      spawnSync("taskkill", ["/pid", String(proc.pid), "/t", "/f"], { stdio: "ignore" });
      return;
    }
    try {
      process.kill(-proc.pid, "SIGKILL");
    } catch (error) {
      if ((error as NodeJS.ErrnoException).code === "ERR_OUT_OF_RANGE") {
        spawnSync("kill", ["-SIGKILL", "--", `-${proc.pid}`], { stdio: "ignore" });
        return;
      }
      if ((error as NodeJS.ErrnoException).code !== "ESRCH") {
        logError(`Failed to kill process group ${proc.pid}: ${(error as Error).message}`);
      }
    }
  }

  function processTreeIsRunning(proc: ChildProcess): boolean {
    if (!proc.pid) return false;
    if (process.platform === "win32") {
      return proc.exitCode === null && proc.signalCode === null;
    }
    try {
      process.kill(-proc.pid, 0);
      return true;
    } catch (error) {
      if ((error as NodeJS.ErrnoException).code === "ERR_OUT_OF_RANGE") {
        return spawnSync("kill", ["-0", "--", `-${proc.pid}`], { stdio: "ignore" }).status === 0;
      }
      return (error as NodeJS.ErrnoException).code !== "ESRCH";
    }
  }

  function runTaskkill(proc: ChildProcess, force: boolean): Promise<void> {
    if (!proc.pid) return Promise.resolve();
    return new Promise((resolveTaskkill) => {
      const args = ["/pid", String(proc.pid), "/t"];
      if (force) args.push("/f");
      const taskkill = spawn("taskkill", args, { stdio: "ignore" });
      const timeout = setTimeout(() => {
        taskkill.kill("SIGKILL");
        resolveTaskkill();
      }, 3000);
      const finish = () => {
        clearTimeout(timeout);
        resolveTaskkill();
      };
      taskkill.once("error", finish);
      taskkill.once("close", finish);
    });
  }

  async function terminateProcessTree(proc: ChildProcess): Promise<void> {
    if (!proc.pid) return;
    if (process.platform === "win32") {
      await runTaskkill(proc, false);
      if (processTreeIsRunning(proc)) await runTaskkill(proc, true);
      return;
    }

    await new Promise<void>((resolveTermination) => {
      let finished = false;
      const finish = () => {
        if (finished) return;
        finished = true;
        clearInterval(exitPoll);
        clearTimeout(forceKillTimer);
        clearTimeout(giveUpTimer);
        resolveTermination();
      };
      const exitPoll = setInterval(() => {
        if (!processTreeIsRunning(proc)) finish();
      }, 25);
      const forceKillTimer = setTimeout(() => signalProcessTree(proc, "SIGKILL"), 2000);
      const giveUpTimer = setTimeout(finish, 3000);
      signalProcessTree(proc, "SIGTERM");
      if (!processTreeIsRunning(proc)) finish();
    });
  }

  /** Start the tygor devtools server */
  async function startDevServer(port: number): Promise<DevServerState> {
    if (disposed) return { process: null, port, ready: false };

    const tygorCmd = getTygorCommand(opts.tygorCommand);
    const rpcDir = resolve(process.cwd(), opts.rpcDir!);
    const args = [...tygorCmd.slice(1), "devtools", "--rpc-dir", rpcDir, "--port", String(port)];
    const command = tygorCmd[0];

    log(`Starting tygor devtools on port ${port}: ${command} ${args.join(" ")}`);

    return new Promise((resolve) => {
      const proc = spawnManaged(command, args, {
        cwd: workdir,
        stdio: ["ignore", "pipe", "pipe"],
      });

      let resolved = false;

      proc.stdout?.on("data", (data) => {
        const output = data.toString();
        process.stdout.write(pc.dim(`[tygor devtools] ${output}`));
        // Check for ready message
        if (!resolved && output.includes("listening on")) {
          resolved = true;
          log(pc.green(`tygor devtools ready on port ${port}`));
          resolve({ process: proc, port, ready: true });
        }
      });

      proc.stderr?.on("data", (data) => {
        process.stderr.write(pc.dim(`[tygor devtools] ${data.toString()}`));
      });

      proc.on("error", (err) => {
        if (!resolved) {
          resolved = true;
          logError(`Failed to start tygor devtools: ${err.message}`);
          resolve({ process: null, port, ready: false });
        }
      });

      proc.on("exit", (code) => {
        if (!resolved) {
          resolved = true;
          logError(`tygor devtools exited with code ${code}`);
          resolve({ process: null, port, ready: false });
        }
      });

      // Timeout after 30 seconds
      schedule(() => {
        if (!resolved) {
          resolved = true;
          logError("tygor devtools startup timed out");
          signalProcessTree(proc, "SIGTERM");
          resolve({ process: null, port, ready: false });
        }
      }, 30000);
    });
  }

  /** Send status update to tygor devtools */
  async function updateDevServerStatus(status: "running" | "building" | "error" | "starting", error?: string, phase?: string) {
    if (disposed || !devServer.ready) return;

    try {
      const client = createClient(devserverRegistry, {
        baseUrl: `http://localhost:${devServer.port}/__tygor`,
      });
      await client.Devtools.UpdateStatus({
        app: {
          status,
          port: currentServer.port,
          error,
          phase,
        },
      });
    } catch (e) {
      log(`Failed to update devserver status: ${e instanceof Error ? e.message : e}`);
    }
  }

  /** Run tygor gen to generate TypeScript types */
  async function runGen(): Promise<boolean> {
    if (disposed) return false;
    if (!opts.gen) return true;

    const tygorCmd = getTygorCommand(opts.tygorCommand);
    const rpcDir = resolve(process.cwd(), opts.rpcDir!);
    const args = [...tygorCmd.slice(1), "gen", rpcDir, "--discovery"];
    const command = tygorCmd[0];
    const fullCmd = `${command} ${args.join(" ")}`;

    log(`Running: ${fullCmd}`);

    return new Promise((resolve) => {
      const proc = spawnManaged(command, args, {
        cwd: workdir,
        stdio: ["ignore", "pipe", "pipe"],
      });

      let stdout = "";
      let stderr = "";

      proc.stdout?.on("data", (data) => {
        stdout += data.toString();
      });

      proc.stderr?.on("data", (data) => {
        stderr += data.toString();
      });

      proc.on("error", (err) => {
        if (disposed) {
          resolve(false);
          return;
        }
        buildError = err.message;
        errorPhase = "gen";
        errorCommand = fullCmd;
        logError(`tygor gen failed: ${err.message}`);
        resolve(false);
      });

      proc.on("exit", (code) => {
        if (disposed) {
          resolve(false);
          return;
        }
        if (code !== 0) {
          buildError = stderr || `tygor gen exited with code ${code}`;
          errorPhase = "gen";
          errorCommand = fullCmd;
          logError(`tygor gen failed:\n${buildError}`);
          resolve(false);
        } else {
          if (stdout.trim()) process.stdout.write(pc.dim(stdout));
          resolve(true);
        }
      });
    });
  }

  async function runPregen(): Promise<boolean> {
    if (disposed) return false;
    if (!opts.pregen) return true;

    const cmd = Array.isArray(opts.pregen) ? opts.pregen.join(" && ") : opts.pregen;
    log(`Running pregen: ${cmd}`);
    return runShellStep(cmd, "Pregen", "prebuild");
  }

  async function runPrebuild(): Promise<boolean> {
    if (disposed) return false;
    if (!opts.prebuild) return true;

    const cmd = Array.isArray(opts.prebuild) ? opts.prebuild.join(" && ") : opts.prebuild;
    log(`Running prebuild: ${cmd}`);
    return runShellStep(cmd, "Prebuild", "prebuild");
  }

  async function runBuild(): Promise<boolean> {
    if (disposed) return false;
    if (!opts.build) {
      log("No build command configured, skipping build step");
      return true;
    }

    // Ensure output directory exists
    if (opts.buildOutput) {
      const outDir = dirname(resolve(workdir, opts.buildOutput));
      log(`Creating output directory: ${outDir}`);
      mkdirSync(outDir, { recursive: true });
    }

    const cmd = Array.isArray(opts.build) ? opts.build.join(" && ") : opts.build;
    log(`Building: ${cmd}`);
    return runShellStep(cmd, "Build", "build");
  }

  async function runShellStep(cmd: string, label: string, phase: "prebuild" | "build"): Promise<boolean> {
    const { error, stdout, stderr } = await execManaged(cmd);
    if (disposed) return false;
    if (error) {
      buildError = stderr || error.message;
      errorPhase = phase;
      errorCommand = cmd;
      logError(`${label} failed:\n${buildError}`);
      return false;
    }
    if (stdout.trim()) console.log(stdout);
    return true;
  }

  async function runBuildPipeline(): Promise<boolean> {
    const steps = [
      ["pregen", runPregen, "Pregen failed"],
      ["gen", runGen, "tygor gen failed"],
      ["prebuild", runPrebuild, "Prebuild failed"],
      ["build", runBuild, "Build failed"],
    ] as const;

    ignoreFileChanges = true;
    try {
      for (const [phase, run, fallbackError] of steps) {
        await updateDevServerStatus("building", undefined, phase);
        if (disposed) return false;
        const ok = await run();
        if (phase === "gen") ignoreFileChanges = false;
        if (disposed) return false;
        if (!ok) {
          await updateDevServerStatus("error", buildError ?? fallbackError, phase);
          return false;
        }
      }
      return true;
    } finally {
      ignoreFileChanges = false;
    }
  }

  async function checkHealth(port: number): Promise<boolean> {
    const url = opts.health
      ? `http://localhost:${port}${opts.health}`
      : `http://localhost:${port}/`;

    try {
      const res = await fetch(url);
      // Any response means server is up (even 404)
      if (process.env.TYGOR_DEBUG) {
        log(`Health check ${port}: ${res.status}`);
      }
      return true;
    } catch (e) {
      // Only log failures
      if (process.env.TYGOR_DEBUG) {
        log(`Health check ${port}: ${e instanceof Error ? e.message : "failed"}`);
      }
      return false;
    }
  }

  function startServer(port: number, retries = 3): Promise<ServerState> {
    if (disposed) return Promise.resolve({ process: null, port, ready: false });

    return new Promise((resolve) => {
      const config = opts.start(port);
      const cmdArray = Array.isArray(config.cmd) ? config.cmd : config.cmd.split(" ");
      const [command, ...args] = cmdArray;

      const env = { ...process.env, ...config.env };
      const spawnCwd = config.cwd ?? workdir;

      log(`Starting server on port ${port}`);

      let proc;
      try {
        proc = spawnManaged(command, args, {
          cwd: spawnCwd,
          env,
          stdio: ["ignore", "pipe", "pipe"],
        });
      } catch (err: unknown) {
        const error = err as NodeJS.ErrnoException;
        // ETXTBSY = binary still being written, retry after delay
        if (error.code === "ETXTBSY" && retries > 0) {
          log(`Binary busy, retrying in 200ms (${retries} retries left)`);
          void lifecycleDelay(200).then((shouldRetry) => {
            if (!shouldRetry || disposed) {
              resolve({ process: null, port, ready: false });
              return;
            }
            startServer(port, retries - 1).then(resolve);
          });
          return;
        }
        logError(`Failed to spawn: ${error.message}`);
        resolve({ process: null, port, ready: false });
        return;
      }

      let stderr = "";
      let resolved = false;

      proc.stdout?.on("data", (data) => {
        process.stdout.write(pc.dim(data.toString()));
      });

      proc.stderr?.on("data", (data) => {
        stderr += data.toString();
        process.stderr.write(pc.dim(data.toString()));
      });

      proc.on("error", (err) => {
        if (!resolved) {
          resolved = true;
          if (disposed) {
            resolve({ process: null, port, ready: false });
            return;
          }
          buildError = err.message;
          errorPhase = "runtime";
          errorCommand = cmdArray.join(" ");
          logError(`Failed to start: ${err.message}`);
          resolve({ process: null, port, ready: false });
        }
      });

      proc.on("exit", (code) => {
        if (!resolved) {
          resolved = true;
          if (!disposed && code !== 0) {
            buildError = stderr || `Process exited with code ${code}`;
            errorPhase = "runtime";
            errorCommand = cmdArray.join(" ");
            errorExitCode = code;
            logError(`Server exited with code ${code}`);
          }
          resolve({ process: null, port, ready: false });
        }
      });

      // Wait for server to be ready - require 2 consecutive successful health checks
      const maxAttempts = 100; // 10 seconds
      let attempts = 0;
      let consecutiveSuccess = 0;

      const pollHealth = async () => {
        attempts++;
        if (resolved || disposed) {
          if (!resolved) {
            resolved = true;
            resolve({ process: null, port, ready: false });
          }
          return;
        }

        const healthy = await checkHealth(port);
        if (disposed) {
          if (!resolved) {
            resolved = true;
            resolve({ process: null, port, ready: false });
          }
          return;
        }
        if (healthy) {
          consecutiveSuccess++;
          if (consecutiveSuccess >= 2) {
            resolved = true;
            log(`Server ready on port ${port}`);
            buildError = null;
            errorPhase = null;
            errorCommand = null;
            errorExitCode = null;
            resolve({ process: proc, port, ready: true });
            return;
          }
        } else {
          consecutiveSuccess = 0;
        }

        if (proc.exitCode !== null) {
          // Process exited - error already handled
          if (!resolved) {
            resolved = true;
            resolve({ process: null, port, ready: false });
          }
        } else if (attempts < maxAttempts) {
          schedule(pollHealth, 100);
        } else {
          resolved = true;
          logError(`Health check timed out after ${attempts * 100}ms`);
          resolve({ process: proc, port, ready: false });
        }
      };

      // Give the process a moment to start
      schedule(pollHealth, 200);
    });
  }

  function killServer(server: ServerState): void {
    if (server.process) {
      log(`Stopping server on port ${server.port}`);
      signalProcessTree(server.process, "SIGTERM");
    }
  }

  // Start server with port retry logic (handles race between findPort and actual bind)
  async function startServerWithRetry(skipPort?: number): Promise<ServerState> {
    const maxAttempts = 5;
    let port = opts.port;

    for (let attempt = 0; attempt < maxAttempts; attempt++) {
      if (disposed) return { process: null, port, ready: false };
      port = await findPort(port);
      if (disposed) return { process: null, port, ready: false };
      if (skipPort && port === skipPort) {
        port = await findPort(port + 1);
        if (disposed) return { process: null, port, ready: false };
      }
      const server = await startServer(port);
      if (server.ready) return server;

      // Port might have been grabbed between findPort and bind - try next
      port++;
      if (attempt < maxAttempts - 1) {
        log(`Port ${server.port} unavailable, trying next...`);
      }
    }
    return { process: null, port, ready: false };
  }

  async function reload() {
    if (disposed || isReloading) return;
    isReloading = true;

    try {
      log("Detected changes, reloading...");
      if (!(await runBuildPipeline())) return;

      // Reload proxy paths in case new services were added
      proxyPaths = loadProxyPaths();
      serviceNames = getServiceNames();

      // Start new server on a different port (skip current server's port)
      await updateDevServerStatus("starting", undefined, "runtime");
      if (disposed) return;
      const candidateServer = await startServerWithRetry(currentServer.port);
      if (disposed) {
        killServer(candidateServer);
        return;
      }
      nextServer = candidateServer;

      if (nextServer.ready) {
        // Swap servers - update currentServer first so proxy routes to new server
        const oldServer = currentServer;
        log(`Swapping: ${oldServer.port} -> ${nextServer.port}`);
        currentServer = nextServer;
        nextServer = null;
        buildError = null;

        log(pc.green(`Switched to port ${currentServer.port}`));
        await updateDevServerStatus("running");
        if (disposed) return;

        // Close any active SSE connections to the old server so clients reconnect to new one
        closeConnectionsForPort(oldServer.port);

        // Give the proxy time to route to new server before killing old one
        schedule(() => {
          log(`Killing old server on port ${oldServer.port}`);
          killServer(oldServer);
        }, 500);
      } else {
        // Keep old server, clean up failed new one
        await updateDevServerStatus("error", buildError ?? "Server start failed", "runtime");
        if (nextServer.process) {
          killServer(nextServer);
        }
        nextServer = null;
      }
    } finally {
      ignoreFileChanges = false;
      isReloading = false;
    }
  }

  // Debounce reload
  let reloadTimeout: ReturnType<typeof setTimeout> | null = null;
  function scheduleReload() {
    if (disposed || ignoreFileChanges) return; // Ignore changes from pregen/gen output
    if (reloadTimeout) {
      clearTimeout(reloadTimeout);
      lifecycleTimers.delete(reloadTimeout);
    }
    reloadTimeout = schedule(() => void reload(), 300);
  }

  // Normalize proxyPrefix: ensure leading slash, no trailing slash
  const proxyPrefix = opts.proxyPrefix === "/" ? "" : opts.proxyPrefix!.replace(/\/$/, "");

  // Load proxy paths from discovery.json
  function loadProxyPaths(): string[] {
    if (opts.proxy && opts.proxy.length > 0) {
      return opts.proxy; // Explicit proxy paths take precedence
    }
    const discoveryPath = resolve(process.cwd(), opts.rpcDir!, "discovery.json");
    try {
      if (existsSync(discoveryPath)) {
        const content = readFileSync(discoveryPath, "utf-8");
        const discovery = JSON.parse(content);
        const services = new Set<string>();
        for (const svc of discovery.Services ?? []) {
          if (svc.name) services.add(`${proxyPrefix}/${svc.name}`);
        }
        if (services.size > 0) {
          log(`Proxy paths from discovery.json: ${[...services].join(", ")}`);
          return [...services];
        }
      }
    } catch (err) {
      log(`Failed to load discovery.json: ${err}`);
    }
    // If no discovery.json but proxyPrefix is set, proxy everything under it
    if (proxyPrefix) {
      log(`No discovery.json, proxying ${proxyPrefix}/*`);
      return [proxyPrefix];
    }
    return [];
  }

  let proxyPaths = loadProxyPaths();

  // Track active proxy connections per port so we can close them on server swap
  // This is needed because SSE connections stay open indefinitely
  const activeConnections = new Map<number, Set<{ req: ClientRequest; res: ServerResponse }>>();

  function trackConnection(port: number, req: ClientRequest, res: ServerResponse) {
    if (!activeConnections.has(port)) {
      activeConnections.set(port, new Set());
    }
    const conn = { req, res };
    activeConnections.get(port)!.add(conn);
    return () => {
      activeConnections.get(port)?.delete(conn);
    };
  }

  function closeConnectionsForPort(port: number) {
    const conns = activeConnections.get(port);
    if (conns && conns.size > 0) {
      log(`Closing ${conns.size} active connection(s) to port ${port}`);
      for (const { req, res } of conns) {
        req.destroy();
        if (!res.writableEnded) {
          res.end();
        }
      }
      conns.clear();
    }
  }

  // Extract service names for mismatch detection (computed once, updated on reload)
  function getServiceNames(): string[] {
    return proxyPaths.map((p) => {
      const withoutPrefix = proxyPrefix ? p.slice(proxyPrefix.length) : p;
      return withoutPrefix.split("/")[1];
    }).filter(Boolean);
  }
  let serviceNames = getServiceNames();

  // Simple heartbeat - just check if server responds
  async function heartbeat(): Promise<boolean> {
    if (!currentServer.ready) return false;
    return checkHealth(currentServer.port);
  }

  // Strip HTTP/2 pseudo-headers (e.g., :method, :path, :scheme, :authority)
  // These are only valid in HTTP/2 and cause errors when used with HTTP/1.1
  function stripPseudoHeaders(headers: IncomingMessage["headers"]): Record<string, string | string[] | undefined> {
    const filtered: Record<string, string | string[] | undefined> = {};
    for (const [key, value] of Object.entries(headers)) {
      if (!key.startsWith(":")) {
        filtered[key] = value;
      }
    }
    return filtered;
  }

  // Strip HTTP/1.1-specific headers that are forbidden in HTTP/2
  // These include connection management headers that don't apply to HTTP/2
  function stripHttp1Headers(headers: IncomingMessage["headers"]): Record<string, string | string[] | undefined> {
    const http1SpecificHeaders = new Set([
      "connection",
      "keep-alive",
      "transfer-encoding",
      "upgrade",
      "proxy-connection",
    ]);

    const filtered: Record<string, string | string[] | undefined> = {};
    for (const [key, value] of Object.entries(headers)) {
      if (!http1SpecificHeaders.has(key.toLowerCase())) {
        filtered[key] = value;
      }
    }
    return filtered;
  }

  // Proxy to tygor devtools server
  function proxyToDevServer(req: IncomingMessage, res: ServerResponse, path?: string) {
    const targetPath = path ?? req.url;
    if (process.env.TYGOR_DEBUG) {
      log(`Proxying ${req.url} -> tygor devtools:${devServer.port}${targetPath}`);
    }

    const proxyReq = httpRequest(
      {
        hostname: "localhost",
        port: devServer.port,
        path: targetPath,
        method: req.method,
        headers: stripPseudoHeaders(req.headers),
      },
      (proxyRes) => {
        res.writeHead(proxyRes.statusCode ?? 500, stripHttp1Headers(proxyRes.headers));
        proxyRes.pipe(res);
      }
    );

    proxyReq.on("error", () => {
      res.writeHead(503, { "Content-Type": "application/json" });
      res.end(JSON.stringify({ error: { code: "devserver_unavailable", message: "tygor devtools is starting..." } }));
    });

    req.pipe(proxyReq);
  }

  // Simple proxy function using Node's http module
  // Strips proxyPrefix when forwarding (Go server is mounted at /)
  function proxyRequest(req: IncomingMessage, res: ServerResponse) {
    const port = currentServer.port;
    // Strip proxyPrefix from the URL before forwarding to Go server
    let targetPath = req.url || "/";
    if (proxyPrefix && targetPath.startsWith(proxyPrefix)) {
      targetPath = targetPath.slice(proxyPrefix.length) || "/";
    }
    log(`Proxying ${req.url} -> localhost:${port}${targetPath}`);

    const proxyReq = httpRequest(
      {
        hostname: "localhost",
        port,
        path: targetPath,
        method: req.method,
        headers: stripPseudoHeaders(req.headers),
      },
      (proxyRes) => {
        // For SSE streams, disable buffering
        res.writeHead(proxyRes.statusCode ?? 500, stripHttp1Headers(proxyRes.headers));

        // Manual piping for better control over SSE/streaming responses
        proxyRes.on("data", (chunk) => {
          res.write(chunk);
          // Flush immediately for SSE - don't let Node buffer
          if (typeof (res as any).flush === "function") {
            (res as any).flush();
          }
        });

        proxyRes.on("end", () => {
          untrack();
          res.end();
        });

        proxyRes.on("close", () => {
          untrack();
          if (!res.writableEnded) {
            res.end();
          }
        });

        proxyRes.on("error", () => {
          untrack();
          if (!res.writableEnded) {
            res.end();
          }
        });
      }
    );

    // Track this connection so we can close it on server swap
    const untrack = trackConnection(port, proxyReq, res);

    proxyReq.on("error", () => {
      untrack();
      if (!res.headersSent) {
        res.writeHead(503, { "Content-Type": "application/json" });
      }
      if (!res.writableEnded) {
        res.end(JSON.stringify({ error: { code: "server_unavailable", message: "Go server is starting..." } }));
      }
    });

    // Handle client disconnect - abort upstream request
    // Note: For POST requests, 'close' fires when request body is fully received,
    // not when client disconnects. We use res.on("close") to detect actual disconnect.
    res.on("close", () => {
      untrack();
      if (!proxyReq.destroyed) {
        proxyReq.destroy();
      }
    });

    req.pipe(proxyReq);
  }

  // HMR for devtools development (hot-swap, not true HMR - state is lost)
  // For true state-preserving HMR, we'd need to serve source files through Vite's
  // module graph with vite-plugin-solid. Options:
  // 1. Run separate Vite dev server in vite-plugin/ with solid plugin, load from there
  // 2. Inject solid transforms for /@tygor/* paths only within this plugin
  // Current approach is good enough since devtools state is mostly ephemeral.
  const VIRTUAL_HMR = "virtual:tygor-hmr";
  const RESOLVED_VIRTUAL_HMR = "\0" + VIRTUAL_HMR;

  let warnedAboutProxyPrefix = false;

  return {
    name: "tygor",

    config(_, { command }) {
      isDev = command === "serve";
    },

    async buildStart() {
      // Run tygor gen during production builds to ensure types are fresh
      if (!isDev && opts.gen) {
        const ok = await runGen();
        if (!ok) {
          throw new Error(`tygor gen failed:\n${buildError}`);
        }
      }
    },

    resolveId(id) {
      if (id === VIRTUAL_HMR) return RESOLVED_VIRTUAL_HMR;
    },

    load(id) {
      if (id === RESOLVED_VIRTUAL_HMR) {
        return `
if (import.meta.hot) {
  import.meta.hot.on("tygor:devtools-update", async () => {
    console.log("[tygor] HMR update received");
    document.getElementById("tygor-devtools")?.remove();
    await import(/* @vite-ignore */ "/@tygor/client.js?t=" + Date.now());
  });
  console.log("[tygor] HMR listener registered");
}
`;
      }
    },

    async configureServer(server: ViteDevServer) {
      // Vite initializes the replacement plugin before closing the previous
      // server. Dispose the prior lifecycle explicitly to prevent overlap.
      const previousLifecycle = activeLifecycles.get(lifecycleKey);
      if (previousLifecycle) await previousLifecycle.dispose();
      if (disposed) return;

      // Resolve rpcDir path for discovery endpoint
      const rpcDir = resolve(process.cwd(), opts.rpcDir!);
      const discoveryPath = resolve(rpcDir, "discovery.json");

      // Route handling
      server.middlewares.use(async (req, res, next) => {
        // Serve HMR listener (processed by Vite for import.meta.hot)
        if (req.url === "/@tygor/hmr.js") {
          const result = await server.transformRequest(VIRTUAL_HMR);
          if (result) {
            res.writeHead(200, { "Content-Type": "application/javascript" });
            res.end(result.code);
            return;
          }
        }

        // Serve devtools client bundle (match with any query params for HMR)
        if (req.url?.startsWith("/@tygor/client.js")) {
          res.writeHead(200, {
            "Content-Type": "application/javascript",
            "Cache-Control": "no-cache",
          });
          // In dev mode, compile on-the-fly from source
          if (devtoolsBundlePath) {
            try {
              const clientSrcDir = resolve(dirname(devtoolsBundlePath), "../client");
              const entryPoint = resolve(clientSrcDir, "index.tsx");
              if (existsSync(entryPoint)) {
                const esbuild = await import("esbuild");
                const { solidPlugin } = await import("esbuild-plugin-solid");
                const result = await esbuild.build({
                  entryPoints: [entryPoint],
                  bundle: true,
                  format: "esm",
                  write: false,
                  plugins: [solidPlugin()],
                  loader: { ".css": "text" },
                  define: { __TYGOR_VERSION__: '"dev"' },
                });
                res.end(result.outputFiles[0].text);
                return;
              }
            } catch (e) {
              log(`Dev bundle failed: ${e instanceof Error ? e.message : e}`);
              // Fall through to static bundle
            }
          }
          res.end(clientBundle);
          return;
        }

        // Proxy /__tygor/* to tygor devtools server (served at /__tygor/Devtools/*)
        if (req.url?.startsWith("/__tygor/")) {
          if (!devServer.ready) {
            res.writeHead(503, { "Content-Type": "application/json" });
            res.end(JSON.stringify({ error: "tygor devtools is starting..." }));
            return;
          }
          // Legacy path rewrites for backwards compatibility
          let targetPath = req.url;
          if (req.url === "/__tygor/discovery") {
            targetPath = "/__tygor/Devtools/GetDiscovery";
          }
          proxyToDevServer(req, res as ServerResponse, targetPath);
          return;
        }

        // Proxy API requests to user's Go server
        if (proxyPaths.some((prefix) => req.url?.startsWith(prefix))) {
          proxyRequest(req, res as ServerResponse);
          return;
        }

        // Detect misconfigured client baseUrl using known service names
        if (!warnedAboutProxyPrefix && req.url) {
          const url = req.url.split("?")[0];
          let mismatchError: string | null = null;

          // Case 1: proxyPrefix is set but client sent without prefix
          // e.g. proxyPrefix="/api", request="/Tasks/List" → mismatch
          if (proxyPrefix) {
            for (const svc of serviceNames) {
              if (url.startsWith(`/${svc}/`)) {
                mismatchError =
                  `Request to ${url} but proxyPrefix is "${opts.proxyPrefix}". ` +
                  `Either remove proxyPrefix from tygor() or add baseUrl: "${opts.proxyPrefix}" to createClient().`;
                break;
              }
            }
          }

          // Case 2: proxyPrefix is "/" but client sent with a prefix
          // e.g. proxyPrefix="/", request="/api/Tasks/List" → check if Tasks is a known service
          if (!proxyPrefix) {
            for (const svc of serviceNames) {
              const idx = url.indexOf(`/${svc}/`);
              if (idx > 0) {
                const prefix = url.slice(0, idx);
                mismatchError =
                  `Request to ${url} but proxyPrefix is "/". ` +
                  `Either add proxyPrefix: "${prefix}" to tygor() or remove baseUrl from createClient().`;
                break;
              }
            }
          }

          if (mismatchError) {
            warnedAboutProxyPrefix = true;
            logError(mismatchError);
            // Also send to devtools
            updateDevServerStatus("error", mismatchError, "config");
          }
        }

        next();
      });

      // Start watcher
      log(`Watching ${opts.watch!.join(", ")} in ${workdir}`);
      let clientWatcher: ReturnType<typeof watch> | null = null;
      if (devtoolsBundlePath) {
        log(`Devtools hot reload enabled`);
        // Watch devtools source files for auto-reload
        const clientSrcDir = resolve(dirname(devtoolsBundlePath), "../client");
        log(`Watching devtools sources in ${clientSrcDir}`);
        clientWatcher = watch(["**/*.tsx", "**/*.ts", "**/*.css"], {
          cwd: clientSrcDir,
          ignoreInitial: true,
        });
        clientWatcher.on("change", (file) => {
          if (disposed) return;
          log(`Devtools changed: ${file}`);
          server.hot.send({ type: "custom", event: "tygor:devtools-update" });
        });
      }
      const watcher = watch(opts.watch!, {
        cwd: workdir,
        ignored: opts.ignore,
        ignoreInitial: true,
      });

      watcher.on("change", (path) => {
        log(`Changed: ${path}`);
        scheduleReload();
      });

      watcher.on("add", (path) => {
        log(`Added: ${path}`);
        scheduleReload();
      });

      watcher.on("unlink", (path) => {
        log(`Removed: ${path}`);
        scheduleReload();
      });

      let watchdogInterval: ReturnType<typeof setInterval> | null = null;
      let cleanupPromise: Promise<void> | null = null;
      let unregisterLifecycle: () => void = () => undefined;
      const onServerClose = () => void cleanup();
      const cleanup = (): Promise<void> => {
        if (cleanupPromise) return cleanupPromise;

        disposed = true;
        log("Shutting down...");
        if (watchdogInterval) clearInterval(watchdogInterval);
        for (const cancel of [...pendingCancellations]) cancel();
        for (const timer of lifecycleTimers) clearTimeout(timer);
        lifecycleTimers.clear();
        reloadTimeout = null;
        server.httpServer?.off("close", onServerClose);

        for (const port of activeConnections.keys()) {
          closeConnectionsForPort(port);
        }

        const processes = [...managedProcesses.values()];
        cleanupPromise = Promise.all([
          watcher.close(),
          clientWatcher?.close() ?? Promise.resolve(),
          ...processes.map(terminateProcessTree),
        ]).then(() => {
          unregisterLifecycle();
        });
        return cleanupPromise;
      };
      const cleanupSync = () => {
        disposed = true;
        if (watchdogInterval) clearInterval(watchdogInterval);
        for (const timer of lifecycleTimers) clearTimeout(timer);
        for (const proc of managedProcesses.values()) killProcessTreeSync(proc);
        unregisterLifecycle();
      };
      dispose = cleanup;
      unregisterLifecycle = registerLifecycle(lifecycleKey, { dispose: cleanup, disposeSync: cleanupSync });

      // Register cleanup before the first await so config reloads during startup
      // cannot escape lifecycle management.
      server.httpServer?.once("close", onServerClose);

      // Start tygor devtools server first
      const devPort = await findPort(9000);
      if (disposed) return;
      const startedDevServer = await startDevServer(devPort);
      if (disposed) {
        if (startedDevServer.process) await terminateProcessTree(startedDevServer.process);
        return;
      }
      devServer = startedDevServer;
      if (!devServer.ready) {
        logError("tygor devtools failed to start");
      }

      if (await runBuildPipeline()) {
        await updateDevServerStatus("starting", undefined, "runtime");
        if (disposed) return;
        const startedServer = await startServerWithRetry();
        if (disposed) {
          killServer(startedServer);
          return;
        }
        currentServer = startedServer;
        if (currentServer.ready) {
          await updateDevServerStatus("running");
        } else {
          await updateDevServerStatus("error", buildError ?? "Server start failed", "runtime");
          logError("Server start failed - fix errors and save to retry");
        }
      }

      // Watchdog: continuously ping server and restart if unresponsive
      let consecutiveFailures = 0;
      const FAILURE_THRESHOLD = 3;

      const watchdog = async () => {
        if (disposed || isReloading || !currentServer.ready) return;

        const alive = await heartbeat();
        if (disposed) return;
        if (alive) {
          consecutiveFailures = 0;
        } else {
          consecutiveFailures++;
          log(`Heartbeat failed (${consecutiveFailures}/${FAILURE_THRESHOLD})`);

          if (consecutiveFailures >= FAILURE_THRESHOLD) {
            logError("Server unresponsive, restarting...");
            consecutiveFailures = 0;
            killServer(currentServer);
            currentServer = { process: null, port: currentServer.port, ready: false };

            // Restart without rebuild (server crashed, not code change)
            const restartedServer = await startServerWithRetry();
            if (disposed) {
              killServer(restartedServer);
              return;
            }
            currentServer = restartedServer;
            if (currentServer.ready) {
              log(pc.green(`Server restarted on port ${currentServer.port}`));
            } else {
              logError("Restart failed - waiting for code change");
            }
          }
        }
      };

      if (disposed) return;
      watchdogInterval = setInterval(watchdog, 2000);
    },

    async closeBundle() {
      await dispose?.();
    },

    transformIndexHtml(html) {
      // Only inject devtools in dev mode
      if (!isDev) return html;

      // Inject devtools bundle + HMR listener (processed by Vite for import.meta.hot)
      const scripts = `
<script type="module" src="/@tygor/client.js"></script>
<script type="module" src="/@tygor/hmr.js"></script>`;
      // Insert before the last </body> tag (case-insensitive)
      const lastBodyIdx = html.toLowerCase().lastIndexOf("</body>");
      if (lastBodyIdx === -1) {
        return html + scripts;
      }
      return html.slice(0, lastBodyIdx) + scripts + html.slice(lastBodyIdx);
    },
  };
}

/** @deprecated Use `tygor` instead */
export const tygorDev = tygor;

export default tygor;
