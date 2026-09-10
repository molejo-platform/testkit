export function createFiniteRequestExecutor({ fetchImpl = globalThis.fetch, timeoutMS = 10_000, setTimeoutImpl = globalThis.setTimeout, clearTimeoutImpl = globalThis.clearTimeout } = {}) {
  let sequence = 0;
  let active;
  async function execute(url, options = {}) {
    const id = ++sequence;
    const controller = new AbortController();
    active = { id, controller };
    const timeout = setTimeoutImpl(() => controller.abort("timeout"), timeoutMS);
    try {
      const response = await fetchImpl(url, { ...options, signal: controller.signal });
      return id === sequence ? { response, stale: false } : { stale: true };
    } catch (error) {
      if (id !== sequence) return { stale: true };
      if (controller.signal.aborted) return { aborted: true, reason: controller.signal.reason === "timeout" ? "timeout" : "cancelled", stale: false };
      throw error;
    } finally {
      clearTimeoutImpl(timeout);
      if (active?.id === id) active = undefined;
    }
  }
  function cancel() {
    if (!active) return false;
    sequence += 1;
    active.controller.abort("cancelled");
    active = undefined;
    return true;
  }
  return { execute, cancel };
}
