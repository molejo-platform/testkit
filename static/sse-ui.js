import { createCorrelationID } from "./correlation-id.js";
import { renderJSONText } from "./json-view.js";

const MAX_EVENTS = 50;

export function mountSSELab(root, {
  locale = "en",
  translate = (key) => key,
  EventSourceImpl = globalThis.EventSource,
  createCorrelationIDImpl = createCorrelationID,
} = {}) {
  const connectButton = root.querySelector("[data-sse-connect]");
  const disconnectButton = root.querySelector("[data-sse-disconnect]");
  const status = root.querySelector("[data-sse-status]");
  const statusText = root.querySelector("[data-sse-status-text]");
  const hint = root.querySelector("[data-sse-hint]");
  const list = root.querySelector("[data-sse-events]");
  const empty = root.querySelector("[data-sse-empty]");
  const count = root.querySelector("[data-sse-count]");
  const events = [];
  let source = null;

  function updateState(state, message) {
    status.className = `connection-status connection-status--${state}`;
    statusText.textContent = message || translate(`sse.${state}`);
    const connected = state === "connected";
    const connecting = state === "connecting";
    connectButton.disabled = connected || connecting;
    disconnectButton.disabled = !connected && !connecting;
    if (state === "connected") hint.textContent = translate("sse.connected_hint");
    else if (state === "error") hint.textContent = translate("sse.error_hint");
    else hint.textContent = translate("sse.disconnected_hint");
  }

  function renderEvent(event) {
    empty.hidden = true;
    const entry = document.createElement("li");
    entry.className = "event-log__entry";
    const meta = document.createElement("div");
    meta.className = "event-log__meta";
    const time = document.createElement("time");
    time.dateTime = new Date().toISOString();
    time.textContent = new Date().toLocaleTimeString(locale, { hour: "2-digit", minute: "2-digit", second: "2-digit" });
    const type = document.createElement("span");
    type.textContent = `${event.type} · ${event.id || "—"}`;
    meta.append(time, type);
    const payload = document.createElement("code");
    payload.className = "event-log__payload";
    renderJSONText(payload, event.data || "");
    entry.append(meta, payload);
    list.append(entry);
    while (list.children.length > MAX_EVENTS + 1) list.removeChild(list.children[1]);
    count.textContent = String(events.length);
    list.scrollTop = list.scrollHeight;
  }

  function receive(event) {
    const normalized = { id: event.lastEventId || event.id || "", type: event.type || "message", data: event.data || "" };
    events.push(normalized);
    if (events.length > MAX_EVENTS) events.shift();
    renderEvent(normalized);
  }

  function connect() {
    if (source) return;
    updateState("connecting", translate("sse.connecting"));
    const correlationID = createCorrelationIDImpl();
    const endpoint = root.dataset.sseUrl || "/events";
    const separator = endpoint.includes("?") ? "&" : "?";
    source = new EventSourceImpl(`${endpoint}${separator}correlation_id=${encodeURIComponent(correlationID)}`);
    source.onopen = () => {
      updateState("connected");
      hint.textContent = `${translate("sse.connected_hint")} correlation_id: ${correlationID}`;
    };
    const onMessage = (event) => receive(event);
    source.onmessage = onMessage;
    if (typeof source.addEventListener === "function") source.addEventListener("status", onMessage);
    source.onerror = () => {
      if (!source) return;
      source.close();
      source = null;
      updateState("error");
    };
  }

  function disconnect() {
    if (source) source.close();
    source = null;
    updateState("disconnected");
  }

  connectButton.addEventListener("click", connect);
  disconnectButton.addEventListener("click", disconnect);
  updateState("disconnected");
}
