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

function uriTLSMode(uri) {
  try { return new URL(uri).searchParams.get("sslmode") || "verify-full"; } catch { return "verify-full"; }
}

export function postgresViewState(current, { databaseRequired = false, connectionID = null } = {}) {
  const useURI = current.mode === "uri";
  const tlsMode = useURI ? uriTLSMode(current.uri) : current.tls;
  return {
    useURI,
    showCredentialSecret: !useURI && current.credential_type !== "none",
    showCA: tlsMode !== "disable",
    showCreate: current.lifecycle === "retained" && !connectionID,
    required: {
      token: true,
      host: !useURI,
      port: !useURI,
      user: !useURI,
      credential_secret: !useURI && current.credential_type !== "none",
      database: !useURI && databaseRequired,
      uri: useURI,
    },
  };
}

export function postgresFormValidation(current, options = {}) {
  const state = postgresViewState(current, options);
  if (!current.token?.trim()) return { field: "token", messageKey: "postgres.validation_token" };
  if (state.useURI) {
    try {
      const uri = new URL(current.uri);
      if (!uri.hostname || (uri.protocol !== "postgres:" && uri.protocol !== "postgresql:")) throw new Error("invalid URI");
    } catch {
      return { field: "uri", messageKey: "postgres.validation_uri" };
    }
    return null;
  }
  if (!current.host?.trim()) return { field: "host", messageKey: "postgres.validation_host" };
  const port = Number(current.port);
  if (!Number.isInteger(port) || port < 1 || port > 65535) return { field: "port", messageKey: "postgres.validation_port" };
  if (!current.user?.trim()) return { field: "user", messageKey: "postgres.validation_user" };
  if (state.required.credential_secret && !current.credential_secret) {
    return { field: "credential_secret", messageKey: "postgres.validation_credential_secret" };
  }
  if (state.required.database && !current.database?.trim()) {
    return { field: "database", messageKey: "postgres.validation_database" };
  }
  return null;
}

const postgresResultKeys = {
  unauthorized: ["postgres.result_unauthorized", "postgres.result_unauthorized_detail"],
  authentication_failed: ["postgres.result_authentication_failed", "postgres.result_authentication_failed_detail"],
  database_unavailable: ["postgres.result_database_unavailable", "postgres.result_database_unavailable_detail"],
  invalid_ca: ["postgres.result_tls_failed", "postgres.result_tls_failed_detail"],
  deadline_exceeded: ["postgres.result_timeout", "postgres.result_timeout_detail"],
  network_timeout: ["postgres.result_timeout", "postgres.result_timeout_detail"],
  connection_failed: ["postgres.result_connection_failed", "postgres.result_connection_failed_detail"],
};

export function postgresResultPresentation(response, body) {
  const result = body.result || body;
  const passed = response.ok && result.status === "success";
  const code = String(result.code || body.code || response.status);
  if (passed) {
    return { passed, code, titleKey: "postgres.result_success", detailKey: "postgres.result_success_detail" };
  }
  const validationFailure = code.startsWith("invalid_") || code.startsWith("unsupported_") || code.endsWith("_conflict") || code.endsWith("_requires_creation");
  const [titleKey, detailKey] = postgresResultKeys[code] || (validationFailure
    ? ["postgres.result_invalid", "postgres.result_invalid_detail"]
    : ["postgres.result_failed", "postgres.result_failed_detail"]);
  return { passed, code, titleKey, detailKey };
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
  const secretToggles = [...root.querySelectorAll("[data-postgres-toggle-secret]")];
  const fieldErrors = [...root.querySelectorAll("[data-postgres-error]")];
  const errorByField = new Map(fieldErrors.map((error) => [error.dataset.postgresError, error]));
  const requiredIndicators = [...root.querySelectorAll("[data-postgres-required-indicator]")];
  const caField = root.querySelector("[data-postgres-ca]");
  const capabilitiesWarning = root.querySelector("[data-postgres-capabilities-warning]");
  const operationButtons = [...root.querySelectorAll("[data-postgres-operation]")];
  const connectionControls = [...root.querySelectorAll("[data-postgres-connection-control]")];
  const cancelButton = root.querySelector("[data-postgres-cancel]");
  const clearButton = root.querySelector("[data-postgres-clear]");
  const createButton = root.querySelector("[data-postgres-create]");
  const destroyButton = root.querySelector("[data-postgres-destroy]");
  const output = root.querySelector("[data-postgres-output]");
  const summary = root.querySelector("[data-postgres-summary]");
  const status = root.querySelector("[data-postgres-status]");
  const resultSummary = root.querySelector("[data-postgres-result-summary]");
  const resultTitle = root.querySelector("[data-postgres-result-title]");
  const resultDetail = root.querySelector("[data-postgres-result-detail]");
  const resultCode = root.querySelector("[data-postgres-result-code]");
  const responseOutput = root.querySelector("[data-postgres-response]");
  const totalDuration = root.querySelector("[data-postgres-total-duration]");
  const diagnosticDuration = root.querySelector("[data-postgres-diagnostic-duration]");
  const version = root.querySelector("[data-postgres-version]");
  const correlationIDOutput = root.querySelector("[data-postgres-correlation-id]");
  const executor = createFiniteRequestExecutor({ fetchImpl, timeoutMS });
  let connectionID = null;
  let connectionValues = null;
  let running = false;
  let requestSequence = 0;
  let databaseRequired = false;
  let activeButton = null;
  let activeButtonLabel = "";

  function values() { return Object.fromEntries(new FormData(form).entries()); }
  function visibleValues() { return connectionID && connectionValues ? { ...connectionValues, ...values() } : values(); }
  function isRetained() { return visibleValues().lifecycle === "retained"; }
  function setSecretVisible(toggle, visible) {
    const input = form.elements.namedItem(toggle.dataset.postgresToggleSecret);
    if (!input) return;
    input.type = visible ? "text" : "password";
    toggle.setAttribute("aria-pressed", String(visible));
    toggle.setAttribute("aria-label", visible ? toggle.dataset.labelHide : toggle.dataset.labelShow);
    const iconShow = toggle.querySelector("[data-password-icon-show]");
    const iconHide = toggle.querySelector("[data-password-icon-hide]");
    if (iconShow) iconShow.hidden = visible;
    if (iconHide) iconHide.hidden = !visible;
  }
  function clearFieldError(name) {
    const control = form.elements.namedItem(name);
    control?.removeAttribute?.("aria-invalid");
    const error = errorByField.get(name);
    if (error) {
      error.hidden = true;
      error.textContent = "";
    }
  }
  function clearFieldErrors() {
    for (const name of errorByField.keys()) clearFieldError(name);
  }
  function validate(current) {
    clearFieldErrors();
    const failure = connectionID
      ? (!current.token?.trim() ? { field: "token", messageKey: "postgres.validation_token" } : null)
      : postgresFormValidation(current, { databaseRequired });
    if (!failure) return true;
    const control = form.elements.namedItem(failure.field);
    const error = errorByField.get(failure.field);
    control?.setAttribute?.("aria-invalid", "true");
    if (error) {
      error.hidden = false;
      error.textContent = translate(failure.messageKey);
    }
    control?.focus?.();
    return false;
  }
  function setRunning(next, trigger = null) {
    running = next;
    if (next && trigger && !activeButton) {
      activeButton = trigger;
      activeButtonLabel = trigger.textContent;
      trigger.textContent = translate("postgres.running_action");
    }
    if (!next && activeButton) {
      activeButton.textContent = activeButtonLabel;
      activeButton = null;
      activeButtonLabel = "";
    }
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
    status.setAttribute("role", "status");
    status.setAttribute("aria-live", "polite");
    status.textContent = translate("lab.waiting");
    resultSummary.hidden = true;
    resultTitle.textContent = "";
    resultDetail.textContent = "";
    resultCode.textContent = "";
    renderText(responseOutput, translate("lab.run_to_see"));
    summary.textContent = safeSummary(visibleValues());
    resetFacts();
  }
  function updateVisibility(reset = true) {
    const current = visibleValues();
    const viewState = postgresViewState(current, { databaseRequired, connectionID });
    fields.hidden = viewState.useURI;
    uriField.hidden = !viewState.useURI;
    credentialSecret.hidden = !viewState.showCredentialSecret;
    caField.hidden = !viewState.showCA;
    for (const [name, required] of Object.entries(viewState.required)) {
      const control = form.elements.namedItem(name);
      if (control) control.required = required;
    }
    for (const indicator of requiredIndicators) {
      indicator.hidden = !viewState.required[indicator.dataset.postgresRequiredIndicator];
    }
    if (!viewState.showCredentialSecret) {
      const input = form.elements.namedItem("credential_secret");
      input.value = "";
      const toggle = secretToggles.find((candidate) => candidate.dataset.postgresToggleSecret === "credential_secret");
      if (toggle) setSecretVisible(toggle, false);
      clearFieldError("credential_secret");
    }
    if (!viewState.useURI) {
      form.elements.namedItem("uri").value = "";
      clearFieldError("uri");
    }
    if (caField.hidden) form.elements.namedItem("ca_pem").value = "";
    createButton.hidden = !viewState.showCreate;
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
  function showPresentation(presentation) {
    status.className = `lab-result-status ${presentation.passed ? "lab-result-status--ok" : "lab-result-status--error"}`;
    status.setAttribute("role", presentation.passed ? "status" : "alert");
    status.setAttribute("aria-live", presentation.passed ? "polite" : "assertive");
    status.textContent = translate(presentation.titleKey);
    resultSummary.hidden = false;
    resultSummary.className = `diagnostic-result-summary ${presentation.passed ? "diagnostic-result-summary--ok" : "diagnostic-result-summary--error"}`;
    resultTitle.textContent = translate(presentation.titleKey);
    resultDetail.textContent = translate(presentation.detailKey);
    resultCode.textContent = presentation.code;
  }
  function showResult(response, body, started, requestedCorrelationID) {
    const result = body.result || body;
    showPresentation(postgresResultPresentation(response, body));
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
      const timedOut = execution.reason === "timeout";
      showPresentation({
        passed: false,
        code: execution.reason,
        titleKey: timedOut ? "postgres.result_timeout" : "postgres.result_cancelled",
        detailKey: timedOut ? "postgres.result_timeout_detail" : "postgres.cancelled",
      });
      renderText(responseOutput, translate(`postgres.${execution.reason}`));
      return null;
    }
    return execution.response;
  }
  function showNetworkFailure() {
    showPresentation({
      passed: false,
      code: "network_error",
      titleKey: "lab.network_error",
      detailKey: "postgres.network_failed",
    });
    renderText(responseOutput, translate("postgres.network_failed"));
  }
  async function submitDiagnostic({ path, current, body, trigger, onResponse = null, refresh = false }) {
    const requestID = ++requestSequence;
    setRunning(true, trigger);
    resetFacts();
    summary.textContent = safeSummary(current);
    resultSummary.hidden = true;
    status.className = "lab-result-status lab-result-status--running";
    status.setAttribute("role", "status");
    status.setAttribute("aria-live", "polite");
    status.textContent = translate("lab.running");
    renderText(responseOutput, translate("lab.running"));
    let started;
    let correlationID;
    try {
      started = now();
      correlationID = createCorrelationIDImpl();
      correlationIDOutput.textContent = correlationID;
      const response = await execute(path, { method: "POST", headers: headers(current, true, correlationID), body: JSON.stringify(body) });
      if (requestID !== requestSequence) return;
      if (!response) {
        totalDuration.textContent = durationSince(started);
        return;
      }
      const parsedBody = await responseBody(response);
      if (requestID !== requestSequence) return;
      onResponse?.(response, parsedBody);
      showResult(response, parsedBody, started, correlationID);
    } catch {
      if (requestID !== requestSequence) return;
      totalDuration.textContent = durationSince(started);
      showNetworkFailure();
    } finally {
      if (requestID === requestSequence) {
        setRunning(false);
        if (refresh) updateVisibility(false);
      }
    }
  }
  async function run(operation, trigger) {
    const current = values();
    if (!validate(current)) return;
    const path = connectionID ? `/api/diagnostics/postgres/connections/${connectionID}/operations` : "/api/diagnostics/postgres";
    const body = connectionID ? { operation } : { operation, connection: postgresConnectionPayload(current) };
    await submitDiagnostic({ path, current, body, trigger });
  }
  async function createConnection(trigger = createButton) {
    const current = values();
    if (!validate(current)) return;
    await submitDiagnostic({
      path: "/api/diagnostics/postgres/connections",
      current,
      body: { connection: postgresConnectionPayload(current) },
      trigger,
      refresh: true,
      onResponse(response, body) {
        if (response.ok && body.connection?.id && body.result?.status === "success") {
          connectionID = body.connection.id;
          connectionValues = current;
        }
      },
    });
  }
  async function destroyConnection(trigger = destroyButton, { quiet = false } = {}) {
    if (!connectionID) return true;
    const current = values();
    if (!validate(current)) return false;
    const requestID = ++requestSequence;
    setRunning(true, trigger);
    resetFacts();
    let destroyed = false;
    try {
      const response = await execute(`/api/diagnostics/postgres/connections/${connectionID}`, { method: "DELETE", headers: headers(current, false) });
      if (requestID !== requestSequence) return false;
      if (response?.ok) {
        connectionID = null;
        connectionValues = null;
        destroyed = true;
        if (!quiet) {
          showPresentation({ passed: true, code: "connection_destroyed", titleKey: "postgres.destroyed", detailKey: "postgres.destroyed_detail" });
          renderText(responseOutput, translate("postgres.destroyed_detail"));
        }
      } else if (response && !quiet) {
        showResult(response, await responseBody(response), undefined, "");
      }
    } catch {
      if (!quiet) showNetworkFailure();
    } finally {
      if (requestID === requestSequence) {
        setRunning(false);
        updateVisibility(false);
      }
    }
    return destroyed;
  }
  async function applyCapabilities() {
    try {
      const response = await fetchImpl("/api/diagnostics/postgres/capabilities", { headers: { "Accept": "application/json" } });
      if (!response.ok) {
        if (capabilitiesWarning) capabilitiesWarning.hidden = false;
        return;
      }
      const capabilities = await response.json();
      for (const select of root.querySelectorAll("[data-postgres-capability]")) {
        const supported = new Set(capabilities[select.dataset.postgresCapability] || []);
        for (const option of select.children) option.hidden = !supported.has(option.value);
        if (!supported.has(select.value)) {
          const fallback = [...select.children].find((option) => supported.has(option.value));
          if (fallback) select.value = fallback.value;
        }
      }
      databaseRequired = capabilities.database_required === true;
      if (capabilitiesWarning) capabilitiesWarning.hidden = true;
      updateVisibility(false);
    } catch {
      if (capabilitiesWarning) capabilitiesWarning.hidden = false;
    }
  }

  for (const radio of root.querySelectorAll('input[name="mode"]')) radio.addEventListener("change", () => updateVisibility());
  for (const button of operationButtons) button.addEventListener("click", () => run(button.dataset.postgresOperation, button));
  for (const toggle of secretToggles) toggle.addEventListener("click", () => {
    const input = form.elements.namedItem(toggle.dataset.postgresToggleSecret);
    setSecretVisible(toggle, input.type === "password");
  });
  form.addEventListener("input", (event) => {
    if (event.target?.name) clearFieldError(event.target.name);
    updateVisibility();
  });
  cancelButton.addEventListener("click", () => {
    if (!executor.cancel()) return;
    setRunning(false);
    showPresentation({ passed: false, code: "cancelled", titleKey: "postgres.result_cancelled", detailKey: "postgres.cancelled" });
    renderText(responseOutput, translate("postgres.cancelled"));
  });
  createButton.addEventListener("click", () => createConnection(createButton));
  destroyButton.addEventListener("click", () => destroyConnection(destroyButton));
  clearButton.addEventListener("click", async () => {
    const destroyed = await destroyConnection(clearButton, { quiet: true });
    clearFieldErrors();
    for (const toggle of secretToggles) setSecretVisible(toggle, false);
    if (destroyed) {
      form.reset();
      connectionID = null;
      connectionValues = null;
      updateVisibility();
      return;
    }
    for (const name of ["token", "credential_secret", "uri", "ca_pem"]) {
      const control = form.elements.namedItem(name);
      if (control) control.value = "";
    }
    updateVisibility(false);
    resetFacts();
    showPresentation({
      passed: false,
      code: "connection_cleanup_failed",
      titleKey: "postgres.clear_partial",
      detailKey: "postgres.clear_partial_detail",
    });
    renderJSON(responseOutput, { code: "connection_cleanup_failed" });
  });
  mountJSONCopyButtons(root, { translate });
  updateVisibility();
  void applyCapabilities();
}
