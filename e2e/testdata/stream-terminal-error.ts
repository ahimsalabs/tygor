import { createClient, ServerError } from '../../packages/client/runtime.ts';

const baseUrl = process.env.TYGOR_BASE_URL!;
const wantCode = process.env.TYGOR_ERROR_CODE!;
const wantMessage = process.env.TYGOR_ERROR_MESSAGE!;
const expectFirst = process.env.TYGOR_EXPECT_FIRST === 'true';
const expectCompletion = process.env.TYGOR_EXPECT_COMPLETION === 'true';
const firstMessage = process.env.TYGOR_EXPECT_FIRST_MESSAGE!;
const registry = {
  manifest: {},
  metadata: { "Feed.Subscribe": { path: "/Feed/Subscribe", primitive: "stream" } },
} as any;
const iterator = createClient(registry, { baseUrl }).Feed.Subscribe({})[Symbol.asyncIterator]();

if (expectFirst) {
  const first = await iterator.next();
  if (first.done || first.value.id !== 1 || (firstMessage !== '' && first.value.message !== firstMessage)) {
    throw new Error("first read did not produce the event before failure: " + JSON.stringify(first));
  }
}
if (expectCompletion) {
  const completed = await iterator.next();
  if (!completed.done) throw new Error("stream did not complete: " + JSON.stringify(completed));
} else {
  for (const phase of ["pending", "future"]) {
    try {
      const result = await iterator.next();
      throw new Error(phase + " read resolved: " + JSON.stringify(result));
    } catch (error) {
      if (!(error instanceof ServerError)) throw error;
      if (error.code !== wantCode || error.message !== wantMessage || error.httpStatus !== 200) {
        throw new Error(phase + " read rejected with wrong server error: " + JSON.stringify(error));
      }
    }
  }
}
