import { createCorrelationID } from "./correlation-id.js";
import { createFiniteRequestExecutor } from "./finite-request.js";
import { mountJSONCopyButtons, renderJSON, renderText } from "./json-view.js";

function safeSummary(values) {
  if (values.mode === "uri") {
    try {
      const parsed = new URL(values.uri);
      const database = parsed.pathname.slice(1) || "server-selected";
      return `${parsed.hostname}:${parsed.port || "5432"} / ${database}`;
    } catch { return "—"; }
  }
  return values.host ? `${values.host}:${values.port || "5432"} / ${values.database || "server-selected"} / ${values.tls || "verify-full"}` : "—";
}

export function postgresConnectionPayload(current) {
  const lifecycle = { mode: current.lifecycle || "ephemeral" };
  if (current.mode === "uri") {
    return {
      uri: current.uri,
      tls_config: current.ca_pem ? { ca_pem: current.ca_pem } : undefined,
      lifecycle,
    };
  }
  return {
    target: { host: current.host, port: current.port ? Number(current.port) : undefined },
    database: current.database ? { name: current.database } : undefined,
    identity: { user: current.user },
    credential: {
      type: current.credential_type || "none",
      secret: current.credential_type === "none" ? undefined : current.credential_secret || undefined,
    },
    tls_config: { mode: current.tls || "verify-full", ca_pem: current.ca_pem || undefined },
    lifecycle,
  };
}

export function mountPostgresLab(root, {
  translate = (key) => key,
  fetchImpl = globalThis.fetch,
  createCorrelationIDImpl = createCorrelationID,
  now = () => globalThis.performance.now(),
  timeoutMS = 10_000,
} = {}) {
  const form = root.querySelector("[data-postgres-form]");
  const fields = root.querySelector("[data-postgres-fields]");
  const uriField = root.querySelector("[data-postgres-uri]");
  const credentialSecret = root.querySelector("[data-postgres-credential-secret]");
  const credentialSecretInput = form.elements.namedItem("credential_secret");
  const credentialSecretToggle = root.querySelector("[data-postgres-toggle-secret]");
  const passwordIconShow = credentialSecretToggle.querySelector("[data-password-icon-show]");
  const passwordIconHide = credentialSecretToggle.querySelector("[data-password-icon-hide]");
  const caField = root.querySelector("[data-postgres-ca]");
  const operationButtons = [...root.querySelectorAll("[data-postgres-operation]")];
  const connectionControls = [...root.querySelectorAll("[data-postgres-connection-control]")];
  const cancelButton = root.querySelector("[data-postgres-cancel]");
  const clearButton = root.querySelector("[data-postgres-clear]");
  const createButton = root.querySelector("[data-postgres-create]");
  const destroyButton = root.querySelector("[data-postgres-destroy]");
  const output = root.querySelector("[data-postgres-output]");
  const summary = root.querySelector("[data-postgres-summary]");
  const status = root.querySelector("[data-postgres-status]");
  const responseOutput = root.querySelector("[data-postgres-response]");
  const totalDuration = root.querySelector("[data-postgres-total-duration]");
  const diagnosticDuration = root.querySelector("[data-postgres-diagnostic-duration]");
  const version = root.querySelector("[data-postgres-version]");
  const correlationIDOutput = root.querySelector("[data-postgres-correlation-id]");
  const executor = createFiniteRequestExecutor({ fetchImpl, timeoutMS });
  let connectionID = null;
  let running = false;

  function values() { return Object.fromEntries(new FormData(form).entries()); }
  function isRetained() { return values().lifecycle === "retained"; }
  function setCredentialSecretVisible(visible) {
    credentialSecretInput.type = visible ? "text" : "password";
    credentialSecretToggle.setAttribute("aria-pressed", String(visible));
    credentialSecretToggle.setAttribute("aria-label", visible ? credentialSecretToggle.dataset.labelHide : credentialSecretToggle.dataset.labelShow);
    passwordIconShow.hidden = visible;
    passwordIconHide.hidden = !visible;
  }
  function setRunning(next) {
    running = next;
    output.setAttribute("aria-busy", String(next));
    cancelButton.hidden = !next;
    for (const button of operationButtons) button.disabled = next || (isRetained() && !connectionID);
    for (const control of connectionControls) control.disabled = next || Boolean(connectionID);
    createButton.disabled = next || !isRetained() || Boolean(connectionID);
    destroyButton.disabled = next || !connectionID;
    destroyButton.hidden = !connectionID;
    clearButton.disabled = next;
  }
  function resetResult() {
    status.className = "lab-result-status";
    status.textContent = translate("lab.waiting");
    renderText(responseOutput, translate("lab.run_to_see"));
    summary.textContent = safeSummary(values());
    resetFacts();
  }
  function updateVisibility(reset = true) {
    const current = values();
    const useURI = current.mode === "uri";
    let tlsMode = current.tls;
    if (useURI) {
      try { tlsMode = new URL(current.uri).searchParams.get("sslmode") || "verify-full"; } catch { tlsMode = "verify-full"; }
    }
    fields.hidden = useURI;
    uriField.hidden = !useURI;
    credentialSecret.hidden = useURI || current.credential_type === "none";
    caField.hidden = tlsMode === "disable";
    if (credentialSecret.hidden) {
      credentialSecretInput.value = "";
      setCredentialSecretVisible(false);
    }
    if (!useURI) form.elements.namedItem("uri").value = "";
    if (caField.hidden) form.elements.namedItem("ca_pem").value = "";
    createButton.hidden = current.lifecycle !== "retained" || Boolean(connectionID);
    setRunning(running);
    if (reset) resetResult();
  }
  function resetFacts() {
    totalDuration.textContent = "—";
    diagnosticDuration.textContent = "—";
    version.textContent = "—";
    correlationIDOutput.textContent = "—";
  }
  function durationSince(started) {
    if (started === undefined) return "—";
    try {
      return `${Math.max(0, Math.round(now() - started))} ms`;
    } catch {
      return "—";
    }
  }
  function headers(current, json = true, correlationID = "") {
    return {
      "Authorization": `Bearer ${current.token}`,
      ...(json ? { "Content-Type": "application/json" } : {}),
      ...(correlationID ? { "X-Testkit-Correlation-ID": correlationID } : {}),
    };
  }
  async function responseBody(response) {
    try {
      return JSON.parse(await response.text());
    } catch {
      return { code: "invalid_response" };
    }
  }
  function showResult(response, body, started, requestedCorrelationID) {
    const result = body.result || body;
    const passed = response.ok && result.status === "success";
    status.className = `lab-result-status ${passed ? "lab-result-status--ok" : "lab-result-status--error"}`;
    status.textContent = result.code || body.code || `${response.status}`;
    totalDuration.textContent = durationSince(started);
    diagnosticDuration.textContent = Number.isFinite(result.duration_ms) ? `${result.duration_ms} ms` : "—";
    version.textContent = response.headers.get("testkit-version") || translate("lab.not_available");
    correlationIDOutput.textContent = response.headers.get("x-testkit-correlation-id") || result.check_id || requestedCorrelationID;
    renderJSON(responseOutput, body);
  }
  async function execute(path, options) {
    const execution = await executor.execute(path, options);
    if (execution.stale) return null;
    if (execution.aborted) {
      status.className = "lab-result-status lab-result-status--error";
      status.textContent = translate(`lab.${execution.reason}`);
      renderText(responseOutput, translate(`postgres.${execution.reason}`));
      return null;
    }
    return execution.response;
  }
  async function run(operation) {
    const current = values();
    setRunning(true);
    resetFacts();
    summary.textContent = safeSummary(current);
    status.className = "lab-result-status lab-result-status--running";
    status.textContent = translate("lab.running");
    renderText(responseOutput, translate("lab.running"));
    let started;
    let correlationID;
    try {
      started = now();
      correlationID = createCorrelationIDImpl();
      correlationIDOutput.textContent = correlationID;
      const path = connectionID ? `/api/diagnostics/postgres/connections/${connectionID}/operations` : "/api/diagnostics/postgres";
      const body = connectionID ? { operation } : { operation, connection: postgresConnectionPayload(current) };
      const response = await execute(path, { method: "POST", headers: headers(current, true, correlationID), body: JSON.stringify(body) });
      if (!response) {
        totalDuration.textContent = durationSince(started);
        return;
      }
      showResult(response, await responseBody(response), started, correlationID);
    } catch {
      totalDuration.textContent = durationSince(started);
      status.className = "lab-result-status lab-result-status--error";
      status.textContent = translate("lab.network_error");
      renderText(responseOutput, translate("postgres.network_failed"));
    } finally { setRunning(false); }
  }
  async function createConnection() {
    const current = values();
    setRunning(true);
    resetFacts();
    let started;
    let correlationID;
    try {
      started = now();
      correlationID = createCorrelationIDImpl();
      correlationIDOutput.textContent = correlationID;
      const response = await execute("/api/diagnostics/postgres/connections", {
        method: "POST", headers: headers(current, true, correlationID), body: JSON.stringify({ connection: postgresConnectionPayload(current) }),
      });
      if (!response) {
        totalDuration.textContent = durationSince(started);
        return;
      }
      const body = await responseBody(response);
      if (response.ok && body.connection?.id && body.result?.status === "success") connectionID = body.connection.id;
      showResult(response, body, started, correlationID);
    } catch {
      totalDuration.textContent = durationSince(started);
      status.className = "lab-result-status lab-result-status--error";
      status.textContent = translate("lab.network_error");
      renderText(responseOutput, translate("postgres.network_failed"));
    } finally { setRunning(false); updateVisibility(false); }
  }
  async function destroyConnection() {
    if (!connectionID) return;
    const current = values();
    setRunning(true);
    try {
      const response = await execute(`/api/diagnostics/postgres/connections/${connectionID}`, { method: "DELETE", headers: headers(current, false) });
      if (response?.ok) {
        connectionID = null;
        status.className = "lab-result-status lab-result-status--ok";
        status.textContent = translate("postgres.destroyed");
        renderText(responseOutput, translate("postgres.destroyed_detail"));
      }
    } finally { setRunning(false); updateVisibility(false); }
  }
  async function applyCapabilities() {
    try {
      const response = await fetchImpl("/api/diagnostics/postgres/capabilities", { headers: { "Accept": "application/json" } });
      if (!response.ok) return;
      const capabilities = await response.json();
      for (const select of root.querySelectorAll("[data-postgres-capability]")) {
        const supported = new Set(capabilities[select.dataset.postgresCapability] || []);
        for (const option of select.children) option.hidden = !supported.has(option.value);
        if (!supported.has(select.value)) {
          const fallback = [...select.children].find((option) => supported.has(option.value));
          if (fallback) select.value = fallback.value;
        }
      }
      const database = form.elements?.namedItem("database");
      if (database) database.required = capabilities.database_required === true;
      updateVisibility(false);
    } catch { /* Static safe defaults remain available. */ }
  }

  for (const radio of root.querySelectorAll('input[name="mode"]')) radio.addEventListener("change", updateVisibility);
  for (const button of operationButtons) button.addEventListener("click", () => run(button.dataset.postgresOperation));
  credentialSecretToggle.addEventListener("click", () => setCredentialSecretVisible(credentialSecretInput.type === "password"));
  form.addEventListener("input", updateVisibility);
  cancelButton.addEventListener("click", () => {
    executor.cancel();
    setRunning(false);
    status.className = "lab-result-status lab-result-status--error";
    status.textContent = translate("lab.cancelled");
    renderText(responseOutput, translate("postgres.cancelled"));
  });
  createButton.addEventListener("click", createConnection);
  destroyButton.addEventListener("click", destroyConnection);
  clearButton.addEventListener("click", async () => { if (connectionID) await destroyConnection(); form.reset(); connectionID = null; setCredentialSecretVisible(false); updateVisibility(); });
  mountJSONCopyButtons(root, { translate });
  updateVisibility();
  void applyCapabilities();
}
