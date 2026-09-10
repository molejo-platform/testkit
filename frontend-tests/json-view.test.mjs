import assert from "node:assert/strict";
import { test } from "node:test";

class FakeElement {
  constructor() {
    this.children = [];
    this.dataset = {};
    this.listeners = new Map();
    this.className = "";
    this._textContent = "";
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

  replaceChildren(...children) {
    this._textContent = "";
    this.children = children;
  }

  addEventListener(type, listener) {
    this.listeners.set(type, listener);
  }

  dispatch(type) {
    return this.listeners.get(type)?.();
  }
}

globalThis.document = {
  createElement: () => new FakeElement(),
};

const { mountJSONCopyButtons, renderJSON, renderJSONText } = await import("../static/json-view.js");

test("renders nested JSON with semantic tokens while preserving formatted text", () => {
  const output = new FakeElement();
  const value = {
    name: "testkit",
    count: 2,
    enabled: true,
    optional: null,
    nested: [false, { version: "rc" }],
  };

  assert.equal(renderJSON(output, value), true);
  assert.equal(output.textContent, JSON.stringify(value, null, 2));
  assert.ok(output.children.some((child) => child.className.includes("json-token--key") && child.textContent === '"name"'));
  assert.ok(output.children.some((child) => child.className.includes("json-token--string") && child.textContent === '"testkit"'));
  assert.ok(output.children.some((child) => child.className.includes("json-token--number") && child.textContent === "2"));
  assert.ok(output.children.some((child) => child.className.includes("json-token--boolean") && child.textContent === "true"));
  assert.ok(output.children.some((child) => child.className.includes("json-token--null") && child.textContent === "null"));
  assert.ok(output.children.some((child) => child.className.includes("json-token--punctuation") && child.textContent === "{"));
});

test("renders compact JSON for event logs", () => {
  const output = new FakeElement();

  renderJSON(output, { state: "connected", attempt: 1 }, { compact: true });

  assert.equal(output.textContent, '{"state":"connected","attempt":1}');
});

test("keeps invalid and malicious-looking input as literal text", () => {
  const output = new FakeElement();
  const input = '<img src=x onerror="alert(1)">';

  assert.equal(renderJSONText(output, input), false);
  assert.equal(output.textContent, input);
  assert.equal(output.children.length, 0);
});

test("copy action copies rendered text and restores its localized label", async () => {
  const root = new FakeElement();
  const button = new FakeElement();
  const output = new FakeElement();
  const copied = [];
  let restore;
  button.textContent = "Copiar JSON";
  button.dataset.jsonCopy = "#result";
  renderJSON(output, { status: "ok" });
  root.querySelectorAll = (selector) => selector === "[data-json-copy]" ? [button] : [];
  root.querySelector = (selector) => selector === "#result" ? output : null;

  mountJSONCopyButtons(root, {
    translate: (key) => ({ "json.copied": "Copiado", "json.copy_failed": "Falha ao copiar" })[key] || key,
    clipboard: { writeText: async (text) => copied.push(text) },
    setTimeoutImpl: (callback) => { restore = callback; },
  });
  await button.dispatch("click");

  assert.deepEqual(copied, [JSON.stringify({ status: "ok" }, null, 2)]);
  assert.equal(button.textContent, "Copiado");
  restore();
  assert.equal(button.textContent, "Copiar JSON");
});
