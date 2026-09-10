import assert from "node:assert/strict";
import { test } from "node:test";
import { createFiniteRequestExecutor } from "../static/finite-request.js";

test("finite executor discards a response after cancellation", async () => {
  let resolveRequest;
  const executor = createFiniteRequestExecutor({
    fetchImpl: () => new Promise((resolve) => { resolveRequest = resolve; }),
    setTimeoutImpl: () => 1,
    clearTimeoutImpl: () => {},
  });
  const pending = executor.execute("/test");
  assert.equal(executor.cancel(), true);
  resolveRequest({ ok: true });
  assert.deepEqual(await pending, { stale: true });
});

test("finite executor aborts a request when its deadline expires", async () => {
  let expire;
  const executor = createFiniteRequestExecutor({
    fetchImpl: (_url, options) => new Promise((_resolve, reject) => {
      options.signal.addEventListener("abort", () => reject(new DOMException("aborted", "AbortError")));
    }),
    setTimeoutImpl: (callback) => { expire = callback; return 1; },
    clearTimeoutImpl: () => {},
  });
  const pending = executor.execute("/test");
  expire();
  assert.deepEqual(await pending, { aborted: true, reason: "timeout", stale: false });
});
