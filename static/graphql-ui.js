import { createFiniteRequestExecutor } from "./finite-request.js";
import { mountJSONCopyButtons, renderJSON, renderJSONText, renderText } from "./json-view.js";

const presets = {
  status: { query: "{ status version }", expectsErrors: false },
  echo: { query: "query Echo($message: String!) { echo(message: $message) }", variables: { message: "hello from GraphQL" }, expectsErrors: false },
  invalid: { query: "query Invalid { missing }", expectsErrors: true },
};

export function mountGraphQLLab(root, { translate = (key) => key, fetchImpl = globalThis.fetch, now = () => globalThis.performance.now(), timeoutMS = 10_000 } = {}) {
  const buttons = [...root.querySelectorAll("[data-graphql-preset]")];
  const sendButton = root.querySelector("[data-graphql-send]");
  const cancelButton = root.querySelector("[data-graphql-cancel]");
  const output = root.querySelector("[data-graphql-output]");
  const requestLine = root.querySelector("[data-graphql-request-line]");
  const status = root.querySelector("[data-graphql-status]");
  const requestOutput = root.querySelector("[data-graphql-request]");
  const responseOutput = root.querySelector("[data-graphql-response]");
  const duration = root.querySelector("[data-graphql-duration]");
  const contentType = root.querySelector("[data-graphql-content-type]");
  const hint = root.querySelector("[data-graphql-hint]");
  const executor = createFiniteRequestExecutor({ fetchImpl, timeoutMS });
  let selectedName = buttons.find((button) => button.className.includes("preset-button--active"))?.dataset.graphqlPreset || "status";
  let running = false;

  function payloadFor(preset) {
    const payload = { query: preset.query };
    if (preset.variables) payload.variables = preset.variables;
    return payload;
  }
  function clearResponse() {
    renderText(responseOutput, translate("lab.run_to_see"));
    status.className = "lab-result-status";
    status.textContent = translate("lab.waiting");
    status.setAttribute("role", "status");
    hint.textContent = translate("graphql.ready");
    duration.textContent = "—";
    contentType.textContent = "—";
  }
  function select(name) {
    const preset = presets[name];
    if (!preset || running) return;
    selectedName = name;
    for (const button of buttons) {
      const selected = button.dataset.graphqlPreset === name;
      button.className = `preset-button${selected ? " preset-button--active" : ""}`;
      button.setAttribute("aria-pressed", String(selected));
    }
    requestLine.textContent = "POST /graphql";
    renderJSON(requestOutput, payloadFor(preset));
    clearResponse();
  }
  function setRunning(next) {
    running = next;
    sendButton.disabled = next;
    cancelButton.hidden = !next;
    output.setAttribute("aria-busy", String(next));
    for (const button of buttons) button.disabled = next;
  }
  async function run() {
    if (running) return;
    const preset = presets[selectedName];
    setRunning(true);
    renderText(responseOutput, translate("lab.running"));
    status.className = "lab-result-status lab-result-status--running";
    status.textContent = translate("lab.running");
    hint.textContent = translate("graphql.running");
    duration.textContent = "—";
    contentType.textContent = "—";
    const started = now();
    try {
      const execution = await executor.execute("/graphql", { method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify(payloadFor(preset)) });
      if (execution.stale) return;
      if (execution.aborted) {
        status.className = "lab-result-status lab-result-status--error";
        status.textContent = translate(`lab.${execution.reason}`);
        hint.textContent = translate(`graphql.${execution.reason}`);
        return;
      }
      const response = execution.response;
      const body = await response.text();
      let parsed;
      try { parsed = JSON.parse(body); } catch {}
      const hasErrors = Array.isArray(parsed?.errors) && parsed.errors.length > 0;
      const passed = response.ok && (preset.expectsErrors ? hasErrors : !hasErrors);
      contentType.textContent = response.headers.get("content-type") || translate("lab.not_available");
      renderJSONText(responseOutput, body);
      status.className = `lab-result-status ${passed ? "lab-result-status--ok" : "lab-result-status--error"}`;
      status.textContent = passed && preset.expectsErrors ? translate("graphql.expected_error") : [response.status, response.statusText].filter(Boolean).join(" ");
      hint.textContent = passed ? translate("graphql.completed") : translate("graphql.failed");
    } catch (error) {
      renderText(responseOutput, error);
      status.className = "lab-result-status lab-result-status--error";
      status.textContent = translate("lab.network_error");
      status.setAttribute("role", "alert");
      hint.textContent = translate("graphql.failed");
    } finally {
      duration.textContent = `${Math.max(0, Math.round(now() - started))} ms`;
      setRunning(false);
    }
  }
  function cancel() {
    if (!executor.cancel()) return;
    status.className = "lab-result-status lab-result-status--error";
    status.textContent = translate("lab.cancelled");
    hint.textContent = translate("graphql.cancelled");
    setRunning(false);
  }
  for (const button of buttons) button.addEventListener("click", () => select(button.dataset.graphqlPreset));
  sendButton.addEventListener("click", run);
  cancelButton.addEventListener("click", cancel);
  root.addEventListener("keydown", (event) => {
    if ((event.ctrlKey || event.metaKey) && event.key === "Enter") { event.preventDefault(); run(); }
  });
  output.setAttribute("aria-busy", "false");
  mountJSONCopyButtons(root, { translate });
  select(selectedName);
}

export { presets as graphQLPresets };
