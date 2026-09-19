import { EventEmitter } from "node:events";
import { existsSync, readFileSync } from "node:fs";
import { dirname } from "node:path";
import { tygor } from "../../dist/index.js";

const [pidFile, fixture, port] = process.argv.slice(2);
const httpServer = new EventEmitter();
const server = {
  httpServer,
  middlewares: { use() {} },
  hot: { send() {} },
  async transformRequest() { return null; },
};
const command = [process.execPath, fixture];
const plugin = tygor({
  gen: false,
  tygorCommand: [...command, "devtools-wrapper", pidFile, "signal"],
  build: [...command, "build", pidFile, "signal"].map(JSON.stringify).join(" "),
  start: (serverPort) => ({
    cmd: [...command, "listener", pidFile, "signal"],
    env: { PORT: String(serverPort) },
  }),
  port: Number(port),
  workdir: dirname(pidFile),
});

await plugin.config({}, { command: "serve", mode: "test" });
void plugin.configureServer(server);
const ready = setInterval(() => {
  if (!existsSync(pidFile)) return;
  const entries = readFileSync(pidFile, "utf8");
  if (entries.includes('"role":"build"')) {
    clearInterval(ready);
    console.log("READY");
  }
}, 20);
