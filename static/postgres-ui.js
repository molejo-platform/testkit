import { createFiniteRequestExecutor } from "./finite-request.js";

function safeSummary(values) {
  if (values.mode === "uri") {
    try {
      const parsed = new URL(values.uri);
      return `${parsed.hostname}:${parsed.port || "5432"} / ${parsed.pathname.slice(1) || "postgres"}`;
    } catch { return "—"; }
  }
  return values.host ? `${values.host}:${values.port || "5432"} / ${values.database || "postgres"} / ${values.tls || "verify-full"}` : "—";
}

export function mountPostgresLab(root, { translate = (key) => key, fetchImpl = globalThis.fetch, timeoutMS = 10_000 } = {}) {
  const form = root.querySelector("[data-postgres-form]");
  const fields = root.querySelector("[data-postgres-fields]");
  const uriField = root.querySelector("[data-postgres-uri]");
  const operationButtons = [...root.querySelectorAll("[data-postgres-operation]")];
  const cancelButton = root.querySelector("[data-postgres-cancel]");
  const clearButton = root.querySelector("[data-postgres-clear]");
  const output = root.querySelector("[data-postgres-output]");
  const summary = root.querySelector("[data-postgres-summary]");
  const status = root.querySelector("[data-postgres-status]");
  const responseOutput = root.querySelector("[data-postgres-response]");
  const executor = createFiniteRequestExecutor({ fetchImpl, timeoutMS });

  function values() { return Object.fromEntries(new FormData(form).entries()); }
  function setRunning(running) {
    output.setAttribute("aria-busy", String(running));
    cancelButton.hidden = !running;
    for (const button of operationButtons) button.disabled = running;
    clearButton.disabled = running;
  }
  function resetResult() {
    status.className = "lab-result-status";
    status.textContent = translate("lab.waiting");
    responseOutput.textContent = translate("lab.run_to_see");
    summary.textContent = safeSummary(values());
  }
  function updateMode() {
    const useURI = values().mode === "uri";
    fields.hidden = useURI;
    uriField.hidden = !useURI;
    resetResult();
  }
  function connectionPayload(current) {
    if (current.mode === "uri") return { uri: current.uri, ca_pem: current.ca_pem || undefined };
    return {
      host: current.host,
      port: current.port ? Number(current.port) : undefined,
      user: current.user,
      password: current.password || undefined,
      database: current.database || undefined,
      tls: current.tls,
      ca_pem: current.ca_pem || undefined,
    };
  }
  async function run(operation) {
    const current = values();
    setRunning(true);
    summary.textContent = safeSummary(current);
    status.className = "lab-result-status lab-result-status--running";
    status.textContent = translate("lab.running");
    responseOutput.textContent = translate("lab.running");
    try {
      const execution = await executor.execute("/api/diagnostics/postgres", {
        method: "POST",
        headers: { "Authorization": `Bearer ${current.token}`, "Content-Type": "application/json" },
        body: JSON.stringify({ operation, connection: connectionPayload(current) }),
      });
      if (execution.stale) return;
      if (execution.aborted) {
        status.className = "lab-result-status lab-result-status--error";
        status.textContent = translate(`lab.${execution.reason}`);
        responseOutput.textContent = translate(`postgres.${execution.reason}`);
        return;
      }
      const response = execution.response;
      const body = await response.json().catch(() => ({ code: "invalid_response" }));
      const passed = response.ok && body.status === "success";
      status.className = `lab-result-status ${passed ? "lab-result-status--ok" : "lab-result-status--error"}`;
      status.textContent = body.code || `${response.status}`;
      responseOutput.textContent = JSON.stringify(body, null, 2);
    } catch {
      status.className = "lab-result-status lab-result-status--error";
      status.textContent = translate("lab.network_error");
      responseOutput.textContent = translate("postgres.network_failed");
    } finally { setRunning(false); }
  }

  for (const radio of root.querySelectorAll('input[name="mode"]')) radio.addEventListener("change", updateMode);
  for (const button of operationButtons) button.addEventListener("click", () => run(button.dataset.postgresOperation));
  form.addEventListener("input", resetResult);
  cancelButton.addEventListener("click", () => {
    executor.cancel();
    setRunning(false);
    status.className = "lab-result-status lab-result-status--error";
    status.textContent = translate("lab.cancelled");
    responseOutput.textContent = translate("postgres.cancelled");
  });
  clearButton.addEventListener("click", () => { form.reset(); updateMode(); });
  updateMode();
}
