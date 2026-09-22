import { appendFileSync, readFileSync, writeFileSync } from "node:fs";
import { createServer } from "node:http";
import { spawn } from "node:child_process";
import { fileURLToPath } from "node:url";
import { dirname, join } from "node:path";

const [mode, pidFile, label] = process.argv.slice(2, 5);
const fixturePath = fileURLToPath(import.meta.url);

function record(role, port) {
  appendFileSync(pidFile, `${JSON.stringify({ label, role, pid: process.pid, port })}\n`);
}

function existingRecords() {
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

function isAlive(pid) {
  try {
    process.kill(pid, 0);
    return true;
  } catch {
    return false;
  }
}

if (
  mode === "devtools-wrapper" ||
  mode === "devtools-delayed-wrapper" ||
  mode === "devtools-stalled-status-wrapper" ||
  mode === "devtools-unready-stubborn-wrapper"
) {
  record("devtools-wrapper");
  const portIndex = process.argv.indexOf("--port");
  const port = process.argv[portIndex + 1];
  const startDevtools = () => {
    const stalled = mode === "devtools-stalled-status-wrapper";
    const unready = mode === "devtools-unready-stubborn-wrapper";
    const role = stalled ? "devtools-stalled-status" : unready ? "devtools-unready" : "devtools";
    spawn(process.execPath, [fixturePath, unready ? "stubborn-listener" : "listener", pidFile, label, port, role], {
      stdio: "inherit",
    });
  };
  if (mode === "devtools-delayed-wrapper") {
    setTimeout(startDevtools, 1500);
  } else {
    startDevtools();
  }
} else if (mode === "listener" || mode === "stubborn-listener" || mode === "watchdog-stubborn" || mode === "hanging-health" || mode === "streaming-health") {
  const port = Number(process.argv[5] ?? process.env.PORT);
  const role = process.argv[6] ?? "backend";
  if (
    mode === "watchdog-stubborn" &&
    existingRecords().some((entry) => entry.label === label && entry.role === "backend" && isAlive(entry.pid))
  ) {
    record("watchdog-overlap", port);
  }
  record(role, port);
  let runningStatusUpdates = 0;
  const server = createServer((req, res) => {
    if (mode === "hanging-health") {
      record("health-stalled", port);
      return;
    }
    if (mode === "streaming-health") {
      record("health-streamed", port);
      res.writeHead(200, { "content-type": "text/plain" });
      res.write("healthy");
      res.socket.on("close", () => record("health-closed", port));
      return;
    }
    if (role === "devtools-stalled-status" && req.url?.endsWith("/Devtools/UpdateStatus")) {
      let body = "";
      req.on("data", (chunk) => (body += chunk));
      req.on("end", () => {
        const status = JSON.parse(body).app?.status;
        if (status === "running" && ++runningStatusUpdates >= 2) {
          record("status-stalled");
          res.writeHead(200, { "content-type": "application/json" });
          res.write('{"result":');
          return;
        }
        res.writeHead(200, { "content-type": "application/json" });
        res.end('{"result":{}}');
      });
      return;
    }
    if (req.headers.accept?.includes("text/event-stream")) {
      res.writeHead(200, { "content-type": "text/event-stream" });
      res.write(`data: ${JSON.stringify({ result: { pid: process.pid } })}\n\n`);
      return;
    }
    res.writeHead(200, { "content-type": "application/json" });
    res.end('{"result":{}}');
  });
  server.listen(port, () => {
    if (role === "devtools" || role === "devtools-stalled-status") console.log(`listening on ${port}`);
  });
  if (mode === "listener" || mode === "hanging-health" || mode === "streaming-health") {
    process.on("SIGTERM", () => server.close());
  } else {
    process.on("SIGTERM", () => undefined);
    setInterval(() => undefined, 1000);
    if (mode === "watchdog-stubborn") setTimeout(() => server.close(), 700);
  }
} else if (mode === "backend-wrapper") {
  const port = Number(process.env.PORT);
  record("backend-wrapper", port);
  const child = spawn(process.execPath, [fixturePath, "listener", pidFile, label, String(port), "backend"], {
    stdio: "ignore",
  });
  child.unref();
  setTimeout(() => undefined, 1000);
} else if (mode === "unready-wrapper") {
  record("candidate-wrapper");
  if (existingRecords().some((entry) => entry.label === label && entry.role === "candidate-child" && isAlive(entry.pid))) {
    record("candidate-overlap");
  }
  const child = spawn(process.execPath, [fixturePath, "linger", pidFile, label], {
    stdio: "ignore",
  });
  child.unref();
  const deadline = Date.now() + 1000;
  const exitAfterChildStarts = () => {
    const childRecorded = existingRecords().some(
      (entry) => entry.label === label && entry.role === "candidate-child" && entry.pid === child.pid,
    );
    if (childRecorded || Date.now() >= deadline) {
      process.exit(1);
    }
    setTimeout(exitAfterChildStarts, 5);
  };
  exitAfterChildStarts();
} else if (mode === "linger") {
  record("candidate-child");
  setInterval(() => undefined, 1000);
} else if (mode === "build") {
  record("build");
  setInterval(() => undefined, 1000);
} else if (mode === "build-delayed") {
  if (existingRecords().some((entry) => entry.label === label && entry.role === "build" && isAlive(entry.pid))) {
    record("build-overlap");
  }
  record("build");
  setTimeout(() => process.exit(0), 700);
} else if (mode === "pregen-delayed") {
  record("pregen");
  setTimeout(() => process.exit(0), 700);
} else if (mode === "pregen-rewrite") {
  record("pregen");
  writeFileSync(join(dirname(pidFile), "generated.go"), `package generated\n// ${Date.now()}\n`);
} else {
  throw new Error(`Unknown fixture mode: ${mode}`);
}
