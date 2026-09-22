import { createClient } from '../../packages/client/runtime.ts';

const registry = {
  manifest: {},
  metadata: { "Feed.Subscribe": { path: "/Feed/Subscribe", primitive: "stream" } },
} as any;
const controller = new AbortController();
const iterator = createClient(registry, { baseUrl: process.env.TYGOR_BASE_URL! })
  .Feed.Subscribe({}, { signal: controller.signal })[Symbol.asyncIterator]();
const pending = iterator.next();
const settled = await Promise.race([
  pending.then((result) => ({ result })),
  Bun.sleep(200).then(() => null),
]);
if (settled !== null) {
  throw new Error("partial transport read resolved: " + JSON.stringify(settled.result));
}
controller.abort();
const cancelled = await pending;
if (!cancelled.done) throw new Error("cancelled read produced a value");
