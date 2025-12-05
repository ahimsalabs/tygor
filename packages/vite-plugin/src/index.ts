import { spawn, ChildProcess, exec } from "node:child_process";
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
  /** Override the tygor command (default: auto-detected local or 'go run ...@latest') */
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
  // Published mode - use go run with version
  // TODO: inject version at build time
  return ["go", "run", "tygor.dev/cmd/tygor@latest"];
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

  let currentServer: ServerState = { process: null, port: opts.port, ready: false };
  let nextServer: ServerState | null = null;
  let devServer: DevServerState = { process: null, port: 9000, ready: false };
  let buildError: string | null = null;
  let errorPhase: "pregen" | "gen" | "prebuild" | "build" | "runtime" | null = null;
  let errorCommand: string | null = null;
  let errorExitCode: number | null = null;
  let currentPhase: "idle" | "prebuild" | "building" | "starting" = "idle";
  let isReloading = false;
  let ignoreFileChanges = false; // Ignore file changes during pregen/gen to prevent loops

  const log = (msg: string) => console.log(pc.cyan("[tygor]"), msg);
  const logError = (msg: string) => console.log(pc.cyan("[tygor]"), pc.red(msg));

  /** Start the tygor devtools server */
  async function startDevServer(port: number): Promise<DevServerState> {
    const tygorCmd = getTygorCommand(opts.tygorCommand);
    const rpcDir = resolve(process.cwd(), opts.rpcDir!);
    const args = [...tygorCmd.slice(1), "devtools", "--rpc-dir", rpcDir, "--port", String(port)];
    const command = tygorCmd[0];

    log(`Starting tygor devtools on port ${port}: ${command} ${args.join(" ")}`);

    return new Promise((resolve) => {
      const proc = spawn(command, args, {
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
      setTimeout(() => {
        if (!resolved) {
          resolved = true;
          logError("tygor devtools startup timed out");
          proc.kill();
          resolve({ process: null, port, ready: false });
        }
      }, 30000);
    });
  }

  /** Send status update to tygor devtools */
  async function updateDevServerStatus(status: "running" | "building" | "error" | "starting", error?: string, phase?: string) {
    if (!devServer.ready) return;

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
    if (!opts.gen) return true;

    const tygorCmd = getTygorCommand(opts.tygorCommand);
    const rpcDir = resolve(process.cwd(), opts.rpcDir!);
    const args = [...tygorCmd.slice(1), "gen", rpcDir, "--discovery"];
    const command = tygorCmd[0];
    const fullCmd = `${command} ${args.join(" ")}`;

    log(`Running: ${fullCmd}`);

    return new Promise((resolve) => {
      const proc = spawn(command, args, {
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
        buildError = err.message;
        errorPhase = "prebuild";
        errorCommand = fullCmd;
        logError(`tygor gen failed: ${err.message}`);
        resolve(false);
      });

      proc.on("exit", (code) => {
        if (code !== 0) {
          buildError = stderr || `tygor gen exited with code ${code}`;
          errorPhase = "prebuild";
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
    if (!opts.pregen) return true;

    const cmd = Array.isArray(opts.pregen) ? opts.pregen.join(" && ") : opts.pregen;
    log(`Running pregen: ${cmd}`);

    return new Promise((resolve) => {
      exec(cmd, { cwd: workdir }, (error, stdout, stderr) => {
        if (error) {
          buildError = stderr || error.message;
          errorPhase = "prebuild";
          errorCommand = cmd;
          logError(`Pregen failed:\n${buildError}`);
          resolve(false);
        } else {
          if (stdout.trim()) console.log(stdout);
          resolve(true);
        }
      });
    });
  }

  async function runPrebuild(): Promise<boolean> {
    if (!opts.prebuild) return true;

    const cmd = Array.isArray(opts.prebuild) ? opts.prebuild.join(" && ") : opts.prebuild;
    log(`Running prebuild: ${cmd}`);

    return new Promise((resolve) => {
      exec(cmd, { cwd: workdir }, (error, stdout, stderr) => {
        if (error) {
          buildError = stderr || error.message;
          errorPhase = "prebuild";
          errorCommand = cmd;
          logError(`Prebuild failed:\n${buildError}`);
          resolve(false);
        } else {
          if (stdout.trim()) console.log(stdout);
          resolve(true);
        }
      });
    });
  }

  async function runBuild(): Promise<boolean> {
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

    return new Promise((resolve) => {
      exec(cmd, { cwd: workdir }, (error, stdout, stderr) => {
        if (error) {
          buildError = stderr || error.message;
          errorPhase = "build";
          errorCommand = cmd;
          logError(`Build failed:\n${buildError}`);
          resolve(false);
        } else {
          if (stdout.trim()) console.log(stdout);
          resolve(true);
        }
      });
    });
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
    return new Promise((resolve) => {
      const config = opts.start(port);
      const cmdArray = Array.isArray(config.cmd) ? config.cmd : config.cmd.split(" ");
      const [command, ...args] = cmdArray;

      const env = { ...process.env, ...config.env };
      const spawnCwd = config.cwd ?? workdir;

      log(`Starting server on port ${port}`);

      let proc;
      try {
        proc = spawn(command, args, {
          cwd: spawnCwd,
          env,
          stdio: ["ignore", "pipe", "pipe"],
        });
      } catch (err: unknown) {
        const error = err as NodeJS.ErrnoException;
        // ETXTBSY = binary still being written, retry after delay
        if (error.code === "ETXTBSY" && retries > 0) {
          log(`Binary busy, retrying in 200ms (${retries} retries left)`);
          setTimeout(() => {
            startServer(port, retries - 1).then(resolve);
          }, 200);
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
          buildError = err.message;
          errorPhase = "runtime";
          errorCommand = cmdArray.join(" ");
          logError(`Failed to start: ${err.message}`);
          resolve({ process: null, port, ready: false });
        }
      });

      proc.on("exit", (code) => {
        if (!resolved && code !== 0 && code !== null) {
          resolved = true;
          buildError = stderr || `Process exited with code ${code}`;
          errorPhase = "runtime";
          errorCommand = cmdArray.join(" ");
          errorExitCode = code;
          logError(`Server exited with code ${code}`);
          resolve({ process: null, port, ready: false });
        }
      });

      // Wait for server to be ready - require 2 consecutive successful health checks
      const maxAttempts = 100; // 10 seconds
      let attempts = 0;
      let consecutiveSuccess = 0;

      const pollHealth = async () => {
        attempts++;
        if (resolved) return;

        const healthy = await checkHealth(port);
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
          setTimeout(pollHealth, 100);
        } else {
          resolved = true;
          logError(`Health check timed out after ${attempts * 100}ms`);
          resolve({ process: proc, port, ready: false });
        }
      };

      // Give the process a moment to start
      setTimeout(pollHealth, 200);
    });
  }

  function killServer(server: ServerState) {
    if (server.process && server.process.exitCode === null) {
      log(`Stopping server on port ${server.port}`);
      server.process.kill("SIGTERM");
    }
  }

  // Start server with port retry logic (handles race between findPort and actual bind)
  async function startServerWithRetry(skipPort?: number): Promise<ServerState> {
    const maxAttempts = 5;
    let port = opts.port;

    for (let attempt = 0; attempt < maxAttempts; attempt++) {
      port = await findPort(port);
      if (skipPort && port === skipPort) {
        port = await findPort(port + 1);
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
    if (isReloading) return;
    isReloading = true;

    log("Detected changes, reloading...");
    await updateDevServerStatus("building", undefined, "pregen");

    // Ignore file changes during pregen/gen to prevent loops from generated files
    ignoreFileChanges = true;

    // Run pregen (e.g., sqlc generate)
    const pregenOk = await runPregen();
    if (!pregenOk) {
      await updateDevServerStatus("error", buildError ?? "Pregen failed", "pregen");
      ignoreFileChanges = false;
      isReloading = false;
      return;
    }

    // Run tygor gen
    await updateDevServerStatus("building", undefined, "gen");
    const genOk = await runGen();

    // Re-enable file watching after codegen completes
    ignoreFileChanges = false;

    if (!genOk) {
      await updateDevServerStatus("error", buildError ?? "tygor gen failed", "gen");
      isReloading = false;
      return;
    }

    // Run prebuild
    const prebuildOk = await runPrebuild();
    if (!prebuildOk) {
      await updateDevServerStatus("error", buildError ?? "Prebuild failed", "prebuild");
      isReloading = false;
      return;
    }

    // Reload proxy paths in case new services were added
    proxyPaths = loadProxyPaths();
    serviceNames = getServiceNames();

    // Run build (separate from start so we can distinguish build vs runtime errors)
    await updateDevServerStatus("building", undefined, "build");
    const buildOk = await runBuild();
    if (!buildOk) {
      await updateDevServerStatus("error", buildError ?? "Build failed", "build");
      isReloading = false;
      return;
    }

    // Start new server on a different port (skip current server's port)
    await updateDevServerStatus("starting", undefined, "runtime");
    nextServer = await startServerWithRetry(currentServer.port);

    if (nextServer.ready) {
      // Swap servers - update currentServer first so proxy routes to new server
      const oldServer = currentServer;
      log(`Swapping: ${oldServer.port} -> ${nextServer.port}`);
      currentServer = nextServer;
      nextServer = null;
      buildError = null;

      log(pc.green(`Switched to port ${currentServer.port}`));
      await updateDevServerStatus("running");

      // Close any active SSE connections to the old server so clients reconnect to new one
      closeConnectionsForPort(oldServer.port);

      // Give the proxy time to route to new server before killing old one
      setTimeout(() => {
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

    isReloading = false;
  }

  // Debounce reload
  let reloadTimeout: ReturnType<typeof setTimeout> | null = null;
  function scheduleReload() {
    if (ignoreFileChanges) return; // Ignore changes from pregen/gen output
    if (reloadTimeout) clearTimeout(reloadTimeout);
    reloadTimeout = setTimeout(reload, 300);
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
        headers: req.headers,
      },
      (proxyRes) => {
        res.writeHead(proxyRes.statusCode ?? 500, proxyRes.headers);
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
        headers: req.headers,
      },
      (proxyRes) => {
        // For SSE streams, disable buffering
        res.writeHead(proxyRes.statusCode ?? 500, proxyRes.headers);

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

  let isDev = false;
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
      if (devtoolsBundlePath) {
        log(`Devtools hot reload enabled`);
        // Watch devtools source files for auto-reload
        const clientSrcDir = resolve(dirname(devtoolsBundlePath), "../client");
        log(`Watching devtools sources in ${clientSrcDir}`);
        const clientWatcher = watch(["**/*.tsx", "**/*.ts", "**/*.css"], {
          cwd: clientSrcDir,
          ignoreInitial: true,
        });
        clientWatcher.on("change", (file) => {
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

      // Start tygor devtools server first
      const devPort = await findPort(9000);
      devServer = await startDevServer(devPort);
      if (!devServer.ready) {
        logError("tygor devtools failed to start");
      }

      // Initial pregen, gen, prebuild, build, and server start
      await updateDevServerStatus("building", undefined, "pregen");

      // Ignore file changes during pregen/gen to prevent loops
      ignoreFileChanges = true;
      const pregenOk = await runPregen();
      if (!pregenOk) {
        await updateDevServerStatus("error", buildError ?? "Pregen failed", "pregen");
        logError("Pregen failed - fix errors and save to retry");
        ignoreFileChanges = false;
      } else {
        await updateDevServerStatus("building", undefined, "gen");
        const genOk = await runGen();
        ignoreFileChanges = false;
        if (!genOk) {
          await updateDevServerStatus("error", buildError ?? "tygor gen failed", "gen");
          logError("tygor gen failed - fix errors and save to retry");
        } else {
          await updateDevServerStatus("building", undefined, "prebuild");
          const prebuildOk = await runPrebuild();
          if (!prebuildOk) {
            await updateDevServerStatus("error", buildError ?? "Prebuild failed", "prebuild");
            logError("Prebuild failed - fix errors and save to retry");
          } else {
            await updateDevServerStatus("building", undefined, "build");
            const buildOk = await runBuild();
            if (buildOk) {
              await updateDevServerStatus("starting", undefined, "runtime");
              currentServer = await startServerWithRetry();
              if (currentServer.ready) {
                await updateDevServerStatus("running");
              } else {
                await updateDevServerStatus("error", buildError ?? "Server start failed", "runtime");
                logError("Server start failed - fix errors and save to retry");
              }
            } else {
              await updateDevServerStatus("error", buildError ?? "Build failed", "build");
              logError("Build failed - fix errors and save to retry");
            }
          }
        }
      }

      // Watchdog: continuously ping server and restart if unresponsive
      let watchdogInterval: ReturnType<typeof setInterval> | null = null;
      let consecutiveFailures = 0;
      const FAILURE_THRESHOLD = 3;

      const watchdog = async () => {
        if (isReloading || !currentServer.ready) return;

        const alive = await heartbeat();
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
            currentServer = await startServerWithRetry();
            if (currentServer.ready) {
              log(pc.green(`Server restarted on port ${currentServer.port}`));
            } else {
              logError("Restart failed - waiting for code change");
            }
          }
        }
      };

      watchdogInterval = setInterval(watchdog, 2000);

      // Cleanup function
      let cleanedUp = false;
      const cleanup = () => {
        if (cleanedUp) return;
        cleanedUp = true;
        log("Shutting down...");
        if (watchdogInterval) clearInterval(watchdogInterval);
        watcher.close();
        killServer(currentServer);
        if (nextServer) killServer(nextServer);
        // Kill tygor devtools
        if (devServer.process && devServer.process.exitCode === null) {
          devServer.process.kill("SIGTERM");
        }
      };

      // Cleanup when Vite's server closes
      server.httpServer?.on("close", cleanup);

      // Also cleanup on process exit (handles SIGTERM, SIGINT, etc.)
      process.on("exit", cleanup);
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
