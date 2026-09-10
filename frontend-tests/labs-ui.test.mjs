import assert from "node:assert/strict";
import { test } from "node:test";

class FakeElement {
  constructor() {
    this.children = [];
    this.dataset = {};
    this.listeners = new Map();
    this.className = "";
    this.textContent = "";
    this.hidden = false;
    this.disabled = false;
    this.value = "";
    this.scrollTop = 0;
    this.scrollHeight = 0;
    this.attributes = new Map();
  }

  addEventListener(type, listener) {
    this.listeners.set(type, listener);
  }

  dispatch(type, event = {}) {
    return this.listeners.get(type)?.({ currentTarget: this, preventDefault() {}, ...event });
  }

  append(...children) {
    this.children.push(...children);
  }

  setAttribute(name, value) {
    this.attributes.set(name, String(value));
  }

  getAttribute(name) {
    return this.attributes.get(name) ?? null;
  }

  removeChild(child) {
    const index = this.children.indexOf(child);
    if (index >= 0) this.children.splice(index, 1);
  }

  querySelector(selector) {
    return this.controls.get(selector);
  }

  querySelectorAll(selector) {
    return this.collections.get(selector) || [];
  }
}

globalThis.document = {
  createElement: () => new FakeElement(),
};

const { mountRestLab, restPresets } = await import("../static/rest-ui.js");
const { mountGraphQLLab, graphQLPresets } = await import("../static/graphql-ui.js");
const { mountSSELab } = await import("../static/sse-ui.js");
const { postgresConnectionPayload } = await import("../static/postgres-ui.js");

const browserCorrelationID = "018f47de-1234-7abc-8def-0123456789ab";

function rootFor(selectors, collections = {}) {
  const root = new FakeElement();
  root.controls = new Map(Object.entries(selectors));
  root.collections = new Map(Object.entries(collections));
  return root;
}

function labTranslator(key) {
  return key;
}

test("PostgreSQL payload separates destination identity credential database TLS and lifecycle", () => {
  const payload = postgresConnectionPayload({
    mode: "fields", host: "db.example", port: "5433", user: "operator",
    credential_type: "token", credential_secret: "sentinel-secret", database: "",
    tls: "verify-full", ca_pem: "certificate", lifecycle: "retained",
  });

  assert.deepEqual(payload, {
    target: { host: "db.example", port: 5433 },
    database: undefined,
    identity: { user: "operator" },
    credential: { type: "token", secret: "sentinel-secret" },
    tls_config: { mode: "verify-full", ca_pem: "certificate" },
    lifecycle: { mode: "retained" },
  });
  assert.equal(JSON.stringify(payload).includes("password"), false);
});

test("PostgreSQL URI remains an explicit alternative and keeps lifecycle separate", () => {
  assert.deepEqual(postgresConnectionPayload({
    mode: "uri", uri: "postgresql://operator@db.example/app", ca_pem: "certificate", lifecycle: "ephemeral",
  }), {
    uri: "postgresql://operator@db.example/app",
    tls_config: { ca_pem: "certificate" },
    lifecycle: { mode: "ephemeral" },
  });
});

function createHTTPRoot(prefix, names = ["status", "items", "echo", "invalid"]) {
  const buttons = names.map((name) => {
    const button = new FakeElement();
    button.dataset[`${prefix}Preset`] = name;
    return button;
  });
  const selectors = {
    [`[data-${prefix}-request-line]`]: new FakeElement(),
    [`[data-${prefix}-status]`]: new FakeElement(),
    [`[data-${prefix}-request]`]: new FakeElement(),
    [`[data-${prefix}-response]`]: new FakeElement(),
    [`[data-${prefix}-duration]`]: new FakeElement(),
    [`[data-${prefix}-content-type]`]: new FakeElement(),
    [`[data-${prefix}-hint]`]: new FakeElement(),
  };
  if (prefix === "rest") {
    selectors["[data-rest-send]"] = new FakeElement();
	selectors["[data-rest-cancel]"] = new FakeElement();
    selectors["[data-rest-output]"] = new FakeElement();
    selectors["[data-rest-size]"] = new FakeElement();
    selectors["[data-rest-version]"] = new FakeElement();
    selectors["[data-rest-correlation-id]"] = new FakeElement();
  }
	if (prefix === "graphql") {
	  selectors["[data-graphql-send]"] = new FakeElement();
	  selectors["[data-graphql-cancel]"] = new FakeElement();
	  selectors["[data-graphql-output]"] = new FakeElement();
	}
  return rootFor(selectors, { [`[data-${prefix}-preset]`]: buttons });
}

test("REST presets select a request without sending it", () => {
  const root = createHTTPRoot("rest");
  const calls = [];

  mountRestLab(root, { fetchImpl: (...args) => calls.push(args), translate: labTranslator });
  root.collections.get("[data-rest-preset]")[2].dispatch("click");

  assert.equal(calls.length, 0);
  assert.equal(root.controls.get("[data-rest-request-line]").textContent, "POST /api/echo");
  assert.equal(root.collections.get("[data-rest-preset]")[2].getAttribute("aria-pressed"), "true");
  assert.equal(root.collections.get("[data-rest-preset]")[0].getAttribute("aria-pressed"), "false");
  assert.equal(root.controls.get("[data-rest-status]").textContent, "lab.waiting");
  assert.equal(root.controls.get("[data-rest-response]").textContent, "rest.run_to_see");
});

test("REST send action preserves the existing contract and renders response facts", async () => {
  const root = createHTTPRoot("rest");
  const calls = [];
  const fetchImpl = async (path, options) => {
    calls.push({ path, options });
    return {
      ok: true,
      status: 200,
      statusText: "OK",
      headers: {
        get: (name) => ({
          "content-type": "application/json",
          "testkit-version": "v0.6.1",
          "x-testkit-correlation-id": browserCorrelationID,
        })[name.toLowerCase()] || null,
      },
      text: async () => '{"status":"ok","version":"dev"}',
    };
  };

  mountRestLab(root, {
    fetchImpl,
    translate: labTranslator,
    createCorrelationIDImpl: () => browserCorrelationID,
    now: (() => {
      const values = [10, 28];
      return () => values.shift();
    })(),
  });
  root.collections.get("[data-rest-preset]")[2].dispatch("click");
  root.controls.get("[data-rest-send]").dispatch("click");
  await new Promise((resolve) => setImmediate(resolve));

  assert.equal(restPresets.echo.method, "POST");
  assert.equal(calls.length, 1);
  assert.equal(calls[0].path, "/api/echo");
  assert.deepEqual(JSON.parse(calls[0].options.body), { hello: "world" });
  assert.equal(calls[0].options.headers["X-Testkit-Correlation-ID"], browserCorrelationID);
  const displayedRequest = JSON.parse(root.controls.get("[data-rest-request]").textContent);
  assert.equal(displayedRequest.correlation_id, browserCorrelationID);
  assert.deepEqual(displayedRequest.body, { hello: "world" });
  assert.equal(root.controls.get("[data-rest-status]").textContent, "200 OK");
  assert.match(root.controls.get("[data-rest-response]").textContent, /"status": "ok"/);
  assert.equal(root.controls.get("[data-rest-duration]").textContent, "18 ms");
  assert.equal(root.controls.get("[data-rest-size]").textContent, "31 B");
  assert.equal(root.controls.get("[data-rest-version]").textContent, "v0.6.1");
  assert.equal(root.controls.get("[data-rest-correlation-id]").textContent, browserCorrelationID);
  assert.equal(root.controls.get("[data-rest-send]").disabled, false);
  assert.equal(root.controls.get("[data-rest-output]").getAttribute("aria-busy"), "false");
});

test("REST invalid JSON preset surfaces the existing error response", async () => {
  const root = createHTTPRoot("rest");
  const fetchImpl = async (_path, options) => {
    assert.equal(options.body, '{"hello":');
    return {
      ok: false,
      status: 400,
      statusText: "Bad Request",
      headers: { get: () => "text/plain; charset=utf-8" },
      text: async () => "invalid JSON request",
    };
  };

  mountRestLab(root, { fetchImpl, translate: labTranslator });
  root.collections.get("[data-rest-preset]")[3].dispatch("click");
  root.controls.get("[data-rest-send]").dispatch("click");
  await new Promise((resolve) => setImmediate(resolve));

  assert.equal(root.controls.get("[data-rest-status]").textContent, "400 Bad Request");
  assert.equal(root.controls.get("[data-rest-response]").textContent, "invalid JSON request");
});

test("REST prevents duplicate sends while a request is running", async () => {
  const root = createHTTPRoot("rest");
  const calls = [];
  let completeRequest;
  const fetchImpl = (...args) => {
    calls.push(args);
    return new Promise((resolve) => {
      completeRequest = resolve;
    });
  };

  mountRestLab(root, { fetchImpl, translate: labTranslator });
  const send = root.controls.get("[data-rest-send]");
  send.dispatch("click");
  send.dispatch("click");

  assert.equal(calls.length, 1);
  assert.equal(send.disabled, true);
  assert.equal(send.textContent, "rest.sending");
  assert.equal(root.controls.get("[data-rest-output]").getAttribute("aria-busy"), "true");

  completeRequest({
    ok: true,
    status: 204,
    statusText: "No Content",
    headers: { get: () => null },
    text: async () => "",
  });
  await new Promise((resolve) => setImmediate(resolve));

  assert.equal(send.disabled, false);
  assert.equal(send.textContent, "rest.send");
});

test("REST reports network failures accessibly and allows retry", async () => {
  const root = createHTTPRoot("rest");
  let calls = 0;
  const fetchImpl = async () => {
    calls += 1;
    throw new TypeError("Failed to fetch");
  };

  mountRestLab(root, { fetchImpl, translate: labTranslator });
  const send = root.controls.get("[data-rest-send]");
  send.dispatch("click");
  await new Promise((resolve) => setImmediate(resolve));

  assert.equal(root.controls.get("[data-rest-status]").textContent, "lab.network_error");
  assert.equal(root.controls.get("[data-rest-status]").getAttribute("role"), "alert");
  assert.equal(root.controls.get("[data-rest-hint]").textContent, "rest.network_failed");
  assert.equal(send.disabled, false);

  send.dispatch("click");
  await new Promise((resolve) => setImmediate(resolve));
  assert.equal(calls, 2);
});

test("REST unlocks and allows retry when local request setup fails", async (context) => {
  const cases = [
    {
      name: "correlation ID generation",
      setup() {
        let attempts = 0;
        return {
          createCorrelationIDImpl() {
            attempts += 1;
            if (attempts === 1) throw new Error("entropy unavailable");
            return browserCorrelationID;
          },
        };
      },
    },
    {
      name: "request clock",
      setup() {
        let attempts = 0;
        return {
          now() {
            attempts += 1;
            if (attempts === 1) throw new Error("clock unavailable");
            return attempts * 10;
          },
        };
      },
    },
  ];

  for (const currentCase of cases) {
    await context.test(currentCase.name, async () => {
      const root = createHTTPRoot("rest");
      let fetchCalls = 0;
      const fetchImpl = async () => {
        fetchCalls += 1;
        return {
          ok: true,
          status: 200,
          statusText: "OK",
          headers: { get: () => null },
          text: async () => "ok",
        };
      };

      mountRestLab(root, { fetchImpl, translate: labTranslator, ...currentCase.setup() });
      const send = root.controls.get("[data-rest-send]");
      await send.dispatch("click");

      assert.equal(fetchCalls, 0);
      assert.equal(send.disabled, false);
      assert.equal(root.controls.get("[data-rest-output]").getAttribute("aria-busy"), "false");
      assert.equal(root.controls.get("[data-rest-status]").textContent, "lab.network_error");

      await send.dispatch("click");
      assert.equal(fetchCalls, 1);
      assert.equal(send.disabled, false);
    });
  }
});

test("REST sends the selected request with the standard keyboard shortcut", async () => {
  const root = createHTTPRoot("rest");
  const calls = [];
  const fetchImpl = async (path) => {
    calls.push(path);
    return {
      ok: true,
      status: 200,
      statusText: "OK",
      headers: { get: () => null },
      text: async () => "ok",
    };
  };
  let prevented = false;

  mountRestLab(root, { fetchImpl, translate: labTranslator });
  root.collections.get("[data-rest-preset]")[1].dispatch("click");
  root.dispatch("keydown", {
    key: "Enter",
    ctrlKey: true,
    preventDefault() {
      prevented = true;
    },
  });
  await new Promise((resolve) => setImmediate(resolve));

  assert.equal(prevented, true);
  assert.deepEqual(calls, ["/api/items"]);
});

test("GraphQL presets require an explicit run and treat expected errors as a passing scenario", async () => {
  const root = createHTTPRoot("graphql", ["status", "echo", "invalid"]);
  const calls = [];
  const fetchImpl = async (_path, options) => {
    calls.push(options);
    return {
      ok: true,
      status: 200,
      statusText: "OK",
      headers: { get: () => "application/json" },
      text: async () => '{"errors":[{"message":"Cannot query field \\"missing\\""}]}',
    };
  };

  mountGraphQLLab(root, { fetchImpl, translate: labTranslator });
  root.collections.get("[data-graphql-preset]")[1].dispatch("click");
	assert.equal(calls.length, 0);
	root.controls.get("[data-graphql-send]").dispatch("click");
  await new Promise((resolve) => setImmediate(resolve));

  const payload = JSON.parse(calls[0].body);
  assert.equal(graphQLPresets.echo.variables.message, "hello from GraphQL");
  assert.match(payload.query, /Echo/);
  assert.deepEqual(payload.variables, { message: "hello from GraphQL" });

  root.collections.get("[data-graphql-preset]")[2].dispatch("click");
	root.controls.get("[data-graphql-send]").dispatch("click");
  await new Promise((resolve) => setImmediate(resolve));
  assert.match(root.controls.get("[data-graphql-response]").textContent, /errors/);
	assert.equal(root.controls.get("[data-graphql-status]").textContent, "graphql.expected_error");
	assert.match(root.controls.get("[data-graphql-status]").className, /--ok/);
});

class FakeEventSource {
  static instances = [];

  constructor(url) {
    this.url = url;
    this.listeners = new Map();
    this.closed = false;
    FakeEventSource.instances.push(this);
  }

  addEventListener(type, listener) {
    this.listeners.set(type, listener);
  }

  emit(type, event = {}) {
    if (type === "status") this.listeners.get("status")?.(event);
    else this[`on${type}`]?.(event);
  }

  close() {
    this.closed = true;
  }
}

test("SSE connects, renders named events and only reconnects explicitly", () => {
  FakeEventSource.instances.length = 0;
  const connect = new FakeElement();
  const disconnect = new FakeElement();
  const root = rootFor({
    "[data-sse-connect]": connect,
    "[data-sse-disconnect]": disconnect,
    "[data-sse-status]": new FakeElement(),
    "[data-sse-status-text]": new FakeElement(),
    "[data-sse-hint]": new FakeElement(),
    "[data-sse-events]": new FakeElement(),
    "[data-sse-empty]": new FakeElement(),
    "[data-sse-count]": new FakeElement(),
  });
  root.dataset.sseUrl = "/events";
  root.controls.get("[data-sse-events]").append(root.controls.get("[data-sse-empty]"));

  mountSSELab(root, {
    EventSourceImpl: FakeEventSource,
    translate: labTranslator,
    createCorrelationIDImpl: () => browserCorrelationID,
  });
  connect.dispatch("click");
  const source = FakeEventSource.instances[0];
  assert.equal(source.url, `/events?correlation_id=${browserCorrelationID}`);
  source.emit("open");
  source.emit("status", { lastEventId: "7", type: "status", data: '{"status":"ok"}' });

  assert.equal(root.controls.get("[data-sse-status-text]").textContent, "sse.connected");
  assert.match(root.controls.get("[data-sse-hint]").textContent, new RegExp(browserCorrelationID));
  assert.equal(root.controls.get("[data-sse-count]").textContent, "1");
  assert.match(root.controls.get("[data-sse-events]").children[1].children[1].textContent, /"status": "ok"/);

  source.emit("error");
  assert.equal(source.closed, true);
  assert.equal(FakeEventSource.instances.length, 1);
  connect.dispatch("click");
  assert.equal(FakeEventSource.instances.length, 2);
});
