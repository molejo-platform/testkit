import { createCorrelationID } from "./correlation-id.js";
import { createFiniteRequestExecutor } from "./finite-request.js";
import { mountJSONCopyButtons, renderJSON, renderJSONText, renderText } from "./json-view.js";

const presets = {
  status: { method: "GET", path: "/api/status" },
  items: { method: "GET", path: "/api/items" },
  echo: { method: "POST", path: "/api/echo", body: JSON.stringify({ hello: "world" }) },
  invalid: { method: "POST", path: "/api/echo", body: '{"hello":' },
};

export function mountRestLab(root, {
  translate = (key) => key,
  fetchImpl = globalThis.fetch,
  createCorrelationIDImpl = createCorrelationID,
  now = () => globalThis.performance.now(),
	timeoutMS = 10_000,
} = {}) {
  const buttons = [...root.querySelectorAll("[data-rest-preset]")];
  const sendButton = root.querySelector("[data-rest-send]");
	const cancelButton = root.querySelector("[data-rest-cancel]");
  const output = root.querySelector("[data-rest-output]");
  const requestLine = root.querySelector("[data-rest-request-line]");
  const status = root.querySelector("[data-rest-status]");
  const requestOutput = root.querySelector("[data-rest-request]");
  const responseOutput = root.querySelector("[data-rest-response]");
  const duration = root.querySelector("[data-rest-duration]");
  const size = root.querySelector("[data-rest-size]");
  const contentType = root.querySelector("[data-rest-content-type]");
  const version = root.querySelector("[data-rest-version]");
  const correlationIDOutput = root.querySelector("[data-rest-correlation-id]");
  const hint = root.querySelector("[data-rest-hint]");
	const executor = createFiniteRequestExecutor({ fetchImpl, timeoutMS });
  let selectedName = buttons.find((button) => button.className.includes("preset-button--active"))?.dataset.restPreset || "status";
  let running = false;

  function requestDetails(preset, correlationID) {
    const request = { method: preset.method, path: preset.path };
    if (correlationID) request.correlation_id = correlationID;
    if (preset.body) {
      try {
        request.body = JSON.parse(preset.body);
      } catch {
        request.body = preset.body;
      }
    }
    return request;
  }

  function clearResponse() {
    renderText(responseOutput, translate("rest.run_to_see"));
    status.className = "lab-result-status";
    status.textContent = translate("lab.waiting");
    status.setAttribute("role", "status");
    hint.textContent = translate("rest.ready");
    duration.textContent = "—";
    size.textContent = "—";
    contentType.textContent = "—";
    version.textContent = "—";
    correlationIDOutput.textContent = "—";
  }

  function select(name) {
    const preset = presets[name];
    if (!preset || running) return;

    selectedName = name;
    for (const button of buttons) {
      const selected = button.dataset.restPreset === name;
      button.className = `preset-button${selected ? " preset-button--active" : ""}`;
      button.setAttribute("aria-pressed", String(selected));
    }
    requestLine.textContent = `${preset.method} ${preset.path}`;
    renderJSON(requestOutput, requestDetails(preset));
    clearResponse();
  }

  function setRunning(nextRunning) {
    running = nextRunning;
    sendButton.disabled = nextRunning;
	cancelButton.hidden = !nextRunning;
    sendButton.textContent = translate(nextRunning ? "rest.sending" : "rest.send");
    output.setAttribute("aria-busy", String(nextRunning));
    for (const button of buttons) button.disabled = nextRunning;
  }

  function durationSince(started) {
    if (started === undefined) return "—";
    try {
      return `${Math.max(0, Math.round(now() - started))} ms`;
    } catch {
      return "—";
    }
  }

  async function run() {
    const preset = presets[selectedName];
    if (!preset || running) return;

    setRunning(true);
    renderText(responseOutput, translate("lab.running"));
    status.className = "lab-result-status lab-result-status--running";
    status.textContent = translate("lab.running");
    status.setAttribute("role", "status");
    hint.textContent = translate("rest.running");
    duration.textContent = "—";
    size.textContent = "—";
    contentType.textContent = "—";
    version.textContent = "—";
    correlationIDOutput.textContent = "—";

    let started;
    try {
      started = now();
      const correlationID = createCorrelationIDImpl();
      renderJSON(requestOutput, requestDetails(preset, correlationID));
      correlationIDOutput.textContent = correlationID;
      const headers = { "X-Testkit-Correlation-ID": correlationID };
      if (preset.body) headers["Content-Type"] = "application/json";
	  const execution = await executor.execute(preset.path, {
        method: preset.method,
        headers,
        body: preset.body,
      });
	  if (execution.stale) return;
	  if (execution.aborted) {
		status.className = "lab-result-status lab-result-status--error";
		status.textContent = translate(`lab.${execution.reason}`);
		hint.textContent = translate(`rest.${execution.reason}`);
		return;
	  }
	  const response = execution.response;
      const body = await response.text();
      duration.textContent = durationSince(started);
      size.textContent = `${new TextEncoder().encode(body).byteLength} B`;
      contentType.textContent = response.headers.get("content-type") || translate("lab.not_available");
      version.textContent = response.headers.get("testkit-version") || translate("lab.not_available");
      correlationIDOutput.textContent = response.headers.get("x-testkit-correlation-id") || correlationID;
      renderJSONText(responseOutput, body);
      status.className = `lab-result-status ${response.ok ? "lab-result-status--ok" : "lab-result-status--error"}`;
      status.textContent = [response.status, response.statusText].filter(Boolean).join(" ");
      hint.textContent = response.ok ? translate("rest.completed") : translate("rest.failed");
    } catch (error) {
      duration.textContent = durationSince(started);
      renderText(responseOutput, error);
      status.className = "lab-result-status lab-result-status--error";
      status.textContent = translate("lab.network_error");
      status.setAttribute("role", "alert");
      hint.textContent = translate("rest.network_failed");
    } finally {
      setRunning(false);
    }
  }

	function cancel() {
	  if (!executor.cancel()) return;
	  status.className = "lab-result-status lab-result-status--error";
	  status.textContent = translate("lab.cancelled");
	  hint.textContent = translate("rest.cancelled");
	  setRunning(false);
	}

  for (const button of buttons) button.addEventListener("click", () => select(button.dataset.restPreset));
  sendButton.addEventListener("click", run);
	cancelButton.addEventListener("click", cancel);
  root.addEventListener("keydown", (event) => {
    if ((event.ctrlKey || event.metaKey) && event.key === "Enter") {
      event.preventDefault();
      run();
    }
  });

  output.setAttribute("aria-busy", "false");
  mountJSONCopyButtons(root, { translate });
  select(selectedName);
}

export { presets as restPresets };
