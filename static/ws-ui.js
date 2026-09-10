import { createWebSocketClient } from "./ws-client.js";
import { renderJSON } from "./json-view.js";

const MAX_LOG_ENTRIES = 50;

export function formatRelativeTime(timestamp, now = new Date(), translate = (key) => key) {
  const elapsed = Math.max(0, now.getTime() - timestamp.getTime());
  const seconds = Math.floor(elapsed / 1000);

  if (seconds < 60) return translate("time.less_than_minute");

  const minutes = Math.floor(seconds / 60);
  if (minutes < 60) {
    return translate(minutes === 1 ? "time.minute" : "time.minutes", { count: minutes });
  }

  const hours = Math.floor(minutes / 60);
  if (hours < 24) {
    return translate(hours === 1 ? "time.hour" : "time.hours", { count: hours });
  }

  const days = Math.floor(hours / 24);
  return translate(days === 1 ? "time.day" : "time.days", { count: days });
}

function formatClockTime(timestamp, locale) {
  return timestamp.toLocaleTimeString(locale, {
    hour: "2-digit",
    minute: "2-digit",
    second: "2-digit",
  });
}

function messageFromPayload(payload) {
  return payload && typeof payload.message === "string" ? payload.message : JSON.stringify(payload);
}

export function mountWebSocketClient(root, { locale = "en", translate = (key) => key, createCorrelationIDImpl } = {}) {
  const connectButton = root.querySelector("[data-ws-connect]");
  const disconnectButton = root.querySelector("[data-ws-disconnect]");
  const form = root.querySelector("[data-ws-form]");
  const messageInput = root.querySelector("[data-ws-message]");
  const sendButton = root.querySelector("[data-ws-send]");
  const status = root.querySelector("[data-ws-status]");
  const statusText = root.querySelector("[data-ws-status-text]");
  const hint = root.querySelector("[data-ws-hint]");
  const log = root.querySelector("[data-ws-log]");
  const emptyLog = root.querySelector("[data-ws-empty]");
  const chat = root.querySelector("[data-ws-chat]");
  const emptyChat = root.querySelector("[data-ws-chat-empty]");
  const chatNote = root.querySelector("[data-ws-chat-note]");
  const eventLogContainer = root.querySelector("[data-ws-event-log]");
  const eventLogLabel = root.querySelector("[data-ws-event-log-label]");
  const jsonLive = root.querySelector("[data-ws-json-live]");
  const viewSwitcher = root.querySelector("[data-ws-view-label]");
  const clientLabel = root.dataset.wsLabel;
  const tabs = [...root.querySelectorAll("[data-ws-tab]")];
  const panels = [...root.querySelectorAll("[data-ws-panel]")];
  const relativeTimes = [];
  const chatHistory = [];
  let nextChatMessageID = 1;
  let activeView = "json";
  const client = createWebSocketClient({
    url: root.dataset.wsUrl,
    onStateChange: ({ state, detail }) => updateState(state, detail),
    onEvent: (event) => handleEvent(event),
    createCorrelationIDImpl,
  });

  function updateState(state, detail = {}) {
    status.className = `connection-status connection-status--${state}`;
    statusText.textContent = translate(`state.${state}`);
    const connected = state === "connected";
    const connecting = state === "connecting";
    connectButton.disabled = connected || connecting;
    disconnectButton.disabled = !connected && !connecting;
    sendButton.disabled = !connected;
    messageInput.disabled = !connected;

    if (state === "connected") {
      hint.textContent = translate("websocket.connected_hint");
    } else if (state === "error") {
      hint.textContent = detail.message === "Connect this client before sending."
        ? translate("error.not_connected")
        : translate("websocket.error_hint");
    } else if (state === "connecting") {
      hint.textContent = translate("websocket.connecting_hint");
    } else {
      hint.textContent = translate("websocket.disconnected_hint");
    }
  }

  function appendEvent(event) {
    emptyLog.hidden = true;
    const entry = document.createElement("li");
    entry.className = `event-log__entry event-log__entry--${event.type.includes("error") ? "error" : "normal"}`;

    const meta = document.createElement("div");
    meta.className = "event-log__meta";
    const time = document.createElement("time");
    time.dateTime = event.timestamp.toISOString();
    time.textContent = formatClockTime(event.timestamp, locale);
    const type = document.createElement("span");
    type.textContent = event.type;
    meta.append(time, type);

    const payload = document.createElement("code");
    payload.className = "event-log__payload";
    renderJSON(payload, event.detail, { compact: true });
    entry.append(meta, payload);
    log.append(entry);

    while (log.children.length > MAX_LOG_ENTRIES + 1) {
      log.removeChild(log.children[1]);
    }
    log.scrollTop = log.scrollHeight;
  }

  function createChatMessage({ id, event, direction }) {
    const entry = document.createElement("article");
    entry.className = `chat-message chat-message--${direction}`;
    entry.dataset.wsHistoryId = String(id);

    const meta = document.createElement("div");
    meta.className = "chat-message__meta";
    const label = document.createElement("span");
    label.textContent = direction === "sent" ? translate("websocket.sent") : translate("websocket.received");
    const time = document.createElement("time");
    time.dateTime = event.timestamp.toISOString();
    time.dataset.wsRelativeTime = event.timestamp.toISOString();
    time.title = event.timestamp.toISOString();
    time.textContent = `${formatClockTime(event.timestamp, locale)} · ${formatRelativeTime(event.timestamp, new Date(), translate)}`;
    relativeTimes.push(time);
    meta.append(label, time);

    const message = document.createElement("p");
    message.className = "chat-message__body";
    message.textContent = messageFromPayload(event.detail);
    entry.append(meta, message);

    return entry;
  }

  function appendChatMessage(message) {
    emptyChat.hidden = true;
    if ([...chat.children].includes(emptyChat)) {
      chat.removeChild(emptyChat);
    }

    const entry = createChatMessage(message);
    chat.append(entry);

    while (chat.children.length > MAX_LOG_ENTRIES) {
      const removed = chat.children[0];
      const removedTime = removed.querySelector("time");
      if (removedTime) {
        const index = relativeTimes.indexOf(removedTime);
        if (index >= 0) relativeTimes.splice(index, 1);
      }
      chat.removeChild(removed);
    }
    chat.scrollTop = chat.scrollHeight;
  }

  function renderChatHistory() {
    relativeTimes.length = 0;
    const messages = chatHistory.slice(-MAX_LOG_ENTRIES);
    if (messages.length === 0) {
      emptyChat.hidden = false;
      chat.replaceChildren(emptyChat);
    } else {
      emptyChat.hidden = true;
      chat.replaceChildren(...messages.map((message) => createChatMessage(message)));
    }
    chat.scrollTop = chat.scrollHeight;
  }

  function chatNeedsRender() {
    const messages = chatHistory.slice(-MAX_LOG_ENTRIES);
    const children = [...chat.children];
    if (messages.length === 0) return children.length !== 1 || children[0] !== emptyChat;
    if (children.length !== messages.length) return true;

    return children.some((child, index) => child.dataset.wsHistoryId !== String(messages[index].id));
  }

  function recordChatMessage(event, direction) {
    const message = { id: nextChatMessageID, event, direction };
    nextChatMessageID += 1;
    chatHistory.push(message);
    if (chatHistory.length > MAX_LOG_ENTRIES) {
      chatHistory.splice(0, chatHistory.length - MAX_LOG_ENTRIES);
    }
    appendChatMessage(message);
  }

  function updateRelativeTimes(now = new Date()) {
    for (const time of relativeTimes) {
      const timestamp = new Date(time.dataset.wsRelativeTime);
      time.textContent = `${formatClockTime(timestamp, locale)} · ${formatRelativeTime(timestamp, now, translate)}`;
    }
  }

  function handleEvent(event) {
    appendEvent(event);

    if (event.type === "message.sent") {
      recordChatMessage(event, "sent");
      return;
    }

    if (event.type === "broadcast.received") {
      recordChatMessage(event, "received");
    }
  }

  function setView(view) {
    const changed = activeView !== view;
    activeView = view;
    for (const tab of tabs) {
      const selected = tab.dataset.wsTab === view;
      tab.className = `view-tab${selected ? " view-tab--active" : ""}`;
      tab.setAttribute("aria-selected", String(selected));
      tab.tabIndex = selected ? 0 : -1;
    }
    for (const panel of panels) {
      panel.hidden = panel.dataset.wsPanel !== view;
    }
    if (view === "chat" && (changed || chatNeedsRender())) renderChatHistory();
  }

  function moveTab(event) {
    const currentIndex = tabs.findIndex((tab) => tab === event.currentTarget);
    if (currentIndex < 0 || !["ArrowLeft", "ArrowRight"].includes(event.key)) return;
    event.preventDefault();
    const nextIndex = event.key === "ArrowRight"
      ? (currentIndex + 1) % tabs.length
      : (currentIndex - 1 + tabs.length) % tabs.length;
    const nextTab = tabs[nextIndex];
    nextTab.focus();
    setView(nextTab.dataset.wsTab);
  }

  for (const tab of tabs) {
    tab.addEventListener("click", () => setView(tab.dataset.wsTab));
    tab.addEventListener("keydown", moveTab);
  }
  connectButton.textContent = translate("websocket.connect");
  disconnectButton.textContent = translate("websocket.disconnect");
  messageInput.value = translate("websocket.default_message", { client: clientLabel });
  messageInput.setAttribute("aria-label", translate("websocket.message_label"));
  root.querySelector("label").textContent = translate("websocket.message_label");
  sendButton.textContent = translate("websocket.send");
  eventLogContainer.setAttribute("aria-label", `${clientLabel} ${translate("websocket.event_log")}`);
  eventLogLabel.textContent = translate("websocket.event_log");
  jsonLive.textContent = translate("websocket.json_live");
  emptyLog.textContent = translate("websocket.waiting");
  chat.setAttribute("aria-label", `${clientLabel} ${translate("websocket.view.chat")}`);
  emptyChat.textContent = translate("websocket.chat_empty");
  chatNote.textContent = translate("websocket.chat_note");
  viewSwitcher.setAttribute("aria-label", `${clientLabel} ${translate("websocket.view_label")}`);
  tabs.find((tab) => tab.dataset.wsTab === "json").textContent = translate("websocket.view.json");
  tabs.find((tab) => tab.dataset.wsTab === "chat").textContent = translate("websocket.view.chat");
  connectButton.addEventListener("click", () => client.connect());
  disconnectButton.addEventListener("click", () => client.disconnect());
  form.addEventListener("submit", (event) => {
    event.preventDefault();
    const message = messageInput.value.trim();
    if (!message) {
      hint.textContent = translate("websocket.hint_empty");
      messageInput.focus();
      return;
    }
    if (client.send(message)) {
      messageInput.select();
    }
  });

  updateState("disconnected");
  setView("json");
  if (typeof window.setInterval === "function") {
    window.setInterval(() => updateRelativeTimes(), 30000);
  }
}
