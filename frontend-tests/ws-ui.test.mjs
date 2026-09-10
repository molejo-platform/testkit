import assert from "node:assert/strict";
import { test } from "node:test";

class FakeElement {
  constructor() {
    this.children = [];
    this.listeners = new Map();
    this.attributes = new Map();
    this.className = "";
    this.dataset = {};
    this.disabled = false;
    this.hidden = false;
    this.scrollHeight = 0;
    this.scrollTop = 0;
    this._textContent = "";
    this.value = "";
  }

  addEventListener(type, listener) {
    const listeners = this.listeners.get(type) || [];
    listeners.push(listener);
    this.listeners.set(type, listeners);
  }

  dispatch(type, event = {}) {
    for (const listener of this.listeners.get(type) || []) {
      listener({ preventDefault() {}, ...event });
    }
  }

  append(...children) {
    this._textContent = "";
    this.children.push(...children);
  }

  removeChild(child) {
    const index = this.children.indexOf(child);
    if (index >= 0) this.children.splice(index, 1);
    return child;
  }

  replaceChildren(...children) {
    this._textContent = "";
    this.children = children;
  }

  get textContent() {
    return this.children.length > 0
      ? this.children.map((child) => child.textContent).join("")
      : this._textContent;
  }

  set textContent(value) {
    this._textContent = String(value);
    this.children = [];
  }

  querySelector(selector) {
    return this.controls.get(selector);
  }

  querySelectorAll(selector) {
    return this.collections?.get(selector) || [];
  }

  setAttribute(name, value) {
    this.attributes.set(name, value);
  }

  focus() {
    this.focused = true;
  }

  select() {
    this.selected = true;
  }
}

class FakeWebSocket {
  static CONNECTING = 0;
  static OPEN = 1;
  static current = null;

  constructor() {
    this.readyState = FakeWebSocket.CONNECTING;
    this.listeners = new Map();
    this.sent = [];
    FakeWebSocket.current = this;
  }

  addEventListener(type, listener) {
    const listeners = this.listeners.get(type) || [];
    listeners.push(listener);
    this.listeners.set(type, listeners);
  }

  emit(type, event = {}) {
    if (type === "open") this.readyState = FakeWebSocket.OPEN;
    if (type === "close") this.readyState = 3;
    for (const listener of this.listeners.get(type) || []) listener(event);
  }

  send(payload) {
    this.sent.push(payload);
  }

  close() {
    this.readyState = 2;
  }
}

globalThis.WebSocket = FakeWebSocket;
globalThis.window = {
  location: {
    href: "http://testkit.example/",
    protocol: "http:",
  },
};
globalThis.document = {
  createElement: () => new FakeElement(),
};

const { mountWebSocketClient, formatRelativeTime } = await import("../static/ws-ui.js");

const englishTexts = {
  "state.connected": "Connected",
  "state.connecting": "Connecting",
  "state.disconnected": "Disconnected",
  "state.error": "Error",
  "time.day": "1 day ago",
  "time.days": "{count} days ago",
  "time.hour": "1 hour ago",
  "time.hours": "{count} hours ago",
  "time.less_than_minute": "less than a minute ago",
  "time.minute": "1 minute ago",
  "time.minutes": "{count} minutes ago",
  "websocket.chat_empty": "Send a message to see local send and receive timing.",
  "websocket.chat_note": "Times are measured in this browser. The server does not provide sender IDs or network timestamps; every broadcast is shown as received.",
  "websocket.connect": "Connect",
  "websocket.connected_hint": "Messages are sent as { message } and broadcast to every client.",
  "websocket.default_message": "hello from {client}",
  "websocket.disconnected_hint": "Connect before sending a JSON message.",
  "websocket.disconnect": "Disconnect",
  "websocket.error_hint": "Check the event log and connect again.",
  "websocket.event_log": "Event log",
  "websocket.hint_empty": "Enter a message before sending.",
  "websocket.json_live": "JSON / live",
  "websocket.message_label": "Message",
  "error.not_connected": "Connect this client before sending.",
  "websocket.received": "Received",
  "websocket.send": "Send",
  "websocket.sent": "Sent",
  "websocket.view.chat": "Chat",
  "websocket.view.json": "JSON",
  "websocket.view_label": "message view",
  "websocket.waiting": "Waiting for connection…",
  "websocket.connecting_hint": "Opening a same-origin WebSocket connection…",
};

function englishTranslate(key, values = {}) {
  return (englishTexts[key] || key).replace(/\{(\w+)\}/g, (_, name) => String(values[name] ?? `{${name}}`));
}

function createUI() {
  const root = new FakeElement();
  root.dataset.wsUrl = "/ws";
  root.dataset.wsLabel = "Client A";
  const jsonTab = new FakeElement();
  jsonTab.dataset.wsTab = "json";
  const chatTab = new FakeElement();
  chatTab.dataset.wsTab = "chat";
  const jsonPanel = new FakeElement();
  jsonPanel.dataset.wsPanel = "json";
  const chatPanel = new FakeElement();
  chatPanel.dataset.wsPanel = "chat";
  const chat = new FakeElement();
  const eventLog = new FakeElement();
  root.controls = new Map([
    ["[data-ws-connect]", new FakeElement()],
    ["[data-ws-disconnect]", new FakeElement()],
    ["[data-ws-form]", new FakeElement()],
    ["[data-ws-message]", new FakeElement()],
    ["[data-ws-send]", new FakeElement()],
    ["[data-ws-status]", new FakeElement()],
    ["[data-ws-status-text]", new FakeElement()],
    ["[data-ws-hint]", new FakeElement()],
    ["[data-ws-log]", new FakeElement()],
    ["[data-ws-empty]", new FakeElement()],
    ["[data-ws-chat]", chat],
    ["[data-ws-chat-empty]", new FakeElement()],
    ["[data-ws-chat-note]", new FakeElement()],
    ["[data-ws-event-log]", eventLog],
    ["[data-ws-event-log-label]", new FakeElement()],
    ["[data-ws-json-live]", new FakeElement()],
    ["[data-ws-view-label]", new FakeElement()],
    ["label", new FakeElement()],
  ]);
  root.collections = new Map([
    ["[data-ws-tab]", [jsonTab, chatTab]],
    ["[data-ws-panel]", [jsonPanel, chatPanel]],
  ]);
  mountWebSocketClient(root, { locale: "en", translate: englishTranslate });
  return root;
}

test("renders state changes, close information and JSON with textContent", () => {
  const root = createUI();
  const statusText = root.controls.get("[data-ws-status-text]");
  const connectButton = root.controls.get("[data-ws-connect]");
  const messageInput = root.controls.get("[data-ws-message]");
  const form = root.controls.get("[data-ws-form]");
  const log = root.controls.get("[data-ws-log]");

  assert.equal(statusText.textContent, "Disconnected");
  connectButton.dispatch("click");
  const socket = FakeWebSocket.current;
  socket.emit("open");
  assert.equal(statusText.textContent, "Connected");

  const unsafeMessage = '<img src=x onerror="alert(1)">';
  messageInput.value = unsafeMessage;
  form.dispatch("submit");
  assert.deepEqual(JSON.parse(socket.sent[0]), { message: unsafeMessage });

  const received = { message: unsafeMessage, version: "dev" };
  socket.emit("message", { data: JSON.stringify(received) });
  socket.emit("message", { data: JSON.stringify(received) });
  const payload = log.children.at(-1).children.at(-1);
  assert.equal(payload.textContent, JSON.stringify(received));
  assert.equal(payload.innerHTML, undefined);

  const chat = root.controls.get("[data-ws-chat]");
  assert.equal(chat.children.length, 3, "identical server broadcasts remain separate from the local send");
  assert.equal(chat.children[0].className, "chat-message chat-message--sent");
  assert.equal(chat.children[0].children.at(-1).textContent, unsafeMessage);
  assert.equal(chat.children[1].className, "chat-message chat-message--received");
  assert.equal(chat.children[1].children.at(-1).textContent, unsafeMessage);
  assert.equal(chat.children[2].className, "chat-message chat-message--received");
  assert.equal(chat.children[2].children.at(-1).textContent, unsafeMessage);
  assert.match(chat.children[0].children[0].children.at(-1).textContent, /less than a minute ago/);

  socket.emit("close", { code: 1006, reason: "network failure", wasClean: false });
  assert.equal(statusText.textContent, "Disconnected");
  assert.match(log.children.at(-1).children.at(-1).textContent, /1006/);
  assert.match(log.children.at(-1).children.at(-1).textContent, /network failure/);
});

test("separates sent and received chat messages and switches views", () => {
  const root = createUI();
  const connectButton = root.controls.get("[data-ws-connect]");
  const messageInput = root.controls.get("[data-ws-message]");
  const form = root.controls.get("[data-ws-form]");
  const chat = root.controls.get("[data-ws-chat]");
  const jsonTab = root.collections.get("[data-ws-tab]")[0];
  const chatTab = root.collections.get("[data-ws-tab]")[1];
  const panels = root.collections.get("[data-ws-panel]");

  connectButton.dispatch("click");
  const socket = FakeWebSocket.current;
  socket.emit("open");
  messageInput.value = "sent locally";
  form.dispatch("submit");
  socket.emit("message", { data: JSON.stringify({ message: "received remotely", version: "dev" }) });

  assert.equal(chat.children.length, 2);
  assert.equal(chat.children[0].className, "chat-message chat-message--sent");
  assert.equal(chat.children[0].children.at(-1).textContent, "sent locally");
  assert.equal(chat.children[1].className, "chat-message chat-message--received");
  assert.equal(chat.children[1].children.at(-1).textContent, "received remotely");
  assert.match(chat.children[1].children[0].children.at(-1).textContent, /less than a minute ago/);

  chatTab.dispatch("click");
  assert.equal(chatTab.attributes.get("aria-selected"), "true");
  assert.equal(jsonTab.attributes.get("aria-selected"), "false");
  assert.equal(panels[0].hidden, true);
  assert.equal(panels[1].hidden, false);

  jsonTab.dispatch("click");
  assert.equal(panels[0].hidden, false);
  assert.equal(panels[1].hidden, true);
});

test("rebuilds chat history when switching from JSON after the chat view is remounted", () => {
  const root = createUI();
  const connectButton = root.controls.get("[data-ws-connect]");
  const messageInput = root.controls.get("[data-ws-message]");
  const form = root.controls.get("[data-ws-form]");
  const chat = root.controls.get("[data-ws-chat]");
  const chatTab = root.collections.get("[data-ws-tab]")[1];

  connectButton.dispatch("click");
  const socket = FakeWebSocket.current;
  socket.emit("open");
  messageInput.value = "history survives";
  form.dispatch("submit");
  socket.emit("message", { data: JSON.stringify({ message: "history survives", version: "dev" }) });

  chat.replaceChildren();
  chatTab.dispatch("click");

  assert.equal(chat.children.length, 2);
  assert.equal(chat.children[0].className, "chat-message chat-message--sent");
  assert.equal(chat.children[0].children.at(-1).textContent, "history survives");
  assert.equal(chat.children[1].className, "chat-message chat-message--received");
  assert.equal(chat.children[1].children.at(-1).textContent, "history survives");
});

test("preserves existing chat nodes when appending to the live chat region", () => {
  const root = createUI();
  const connectButton = root.controls.get("[data-ws-connect]");
  const messageInput = root.controls.get("[data-ws-message]");
  const form = root.controls.get("[data-ws-form]");
  const chat = root.controls.get("[data-ws-chat]");
  const chatTab = root.collections.get("[data-ws-tab]")[1];

  chatTab.dispatch("click");
  connectButton.dispatch("click");
  const socket = FakeWebSocket.current;
  socket.emit("open");

  messageInput.value = "first message";
  form.dispatch("submit");
  const firstMessage = chat.children[0];

  messageInput.value = "second message";
  form.dispatch("submit");

  assert.equal(chat.children[0], firstMessage);
  assert.equal(chat.children[0].children.at(-1).textContent, "first message");
  assert.equal(chat.children[1].children.at(-1).textContent, "second message");

  chatTab.dispatch("click");
  assert.equal(chat.children[0], firstMessage);
});

test("formats relative timestamps in English", () => {
  const now = new Date("2026-08-23T12:00:00Z");

  assert.equal(formatRelativeTime(new Date("2026-08-23T11:59:45Z"), now, englishTranslate), "less than a minute ago");
  assert.equal(formatRelativeTime(new Date("2026-08-23T11:59:00Z"), now, englishTranslate), "1 minute ago");
  assert.equal(formatRelativeTime(new Date("2026-08-23T11:58:00Z"), now, englishTranslate), "2 minutes ago");
  assert.equal(formatRelativeTime(new Date("2026-08-23T11:00:00Z"), now, englishTranslate), "1 hour ago");
  assert.equal(formatRelativeTime(new Date("2026-08-23T10:00:00Z"), now, englishTranslate), "2 hours ago");
  assert.equal(formatRelativeTime(new Date("2026-08-22T12:00:00Z"), now, englishTranslate), "1 day ago");
});

test("shows Error and prevents sending when the client is disconnected", () => {
  const root = createUI();
  const statusText = root.controls.get("[data-ws-status-text]");
  const messageInput = root.controls.get("[data-ws-message]");
  const form = root.controls.get("[data-ws-form]");

  messageInput.value = "hello";
  form.dispatch("submit");

  assert.equal(statusText.textContent, "Error");
  assert.match(root.controls.get("[data-ws-hint]").textContent, /Connect this client/);
});
