import { appendFileSync } from "node:fs";
import { createServer } from "node:http";
import { spawn } from "node:child_process";
import { fileURLToPath } from "node:url";

const [mode, pidFile, label] = process.argv.slice(2, 5);
const fixturePath = fileURLToPath(import.meta.url);

function record(role, port) {
  appendFileSync(pidFile, `${JSON.stringify({ label, role, pid: process.pid, port })}\n`);
}

if (mode === "devtools-wrapper") {
  record("devtools-wrapper");
  const portIndex = process.argv.indexOf("--port");
  const port = process.argv[portIndex + 1];
  spawn(process.execPath, [fixturePath, "listener", pidFile, label, port, "devtools"], {
    stdio: "inherit",
  });
} else if (mode === "listener") {
  const port = Number(process.argv[5] ?? process.env.PORT);
  const role = process.argv[6] ?? "backend";
  record(role, port);
  const server = createServer((_req, res) => {
    res.writeHead(200, { "content-type": "application/json" });
    res.end('{"result":{}}');
  });
  server.listen(port, () => {
    if (role === "devtools") console.log(`listening on ${port}`);
  });
  process.on("SIGTERM", () => server.close());
} else if (mode === "backend-wrapper") {
  const port = Number(process.env.PORT);
  record("backend-wrapper", port);
  const child = spawn(process.execPath, [fixturePath, "listener", pidFile, label, String(port), "backend"], {
    stdio: "ignore",
  });
  child.unref();
  setTimeout(() => undefined, 1000);
} else if (mode === "build") {
  record("build");
  setInterval(() => undefined, 1000);
} else {
  throw new Error(`Unknown fixture mode: ${mode}`);
}
