import { expect, test } from "@playwright/test";

test("GraphQL uses real templates, keyboard execution, and expected-error semantics", async ({ page }) => {
  await page.goto("/en/graphql-lab");
  await page.getByRole("button", { name: "Invalid field" }).click();
  await expect(page.locator("[data-graphql-response]")).toContainText("Run a scenario");
  await page.locator("[data-graphql-lab]").press("Control+Enter");
  await expect(page.locator("[data-graphql-status]")).toHaveText("Expected GraphQL error");
  await expect(page.locator("[data-graphql-response]")).toContainText("errors");
});

test("REST reports a network error and allows an explicit retry", async ({ page }) => {
  await page.route("**/api/status", (route) => route.abort());
  await page.goto("/en/rest");
  await page.getByRole("button", { name: "Send request" }).click();
  await expect(page.locator("[data-rest-status]")).toHaveText("Network error");
  await page.unroute("**/api/status");
  await page.getByRole("button", { name: "Send request" }).click();
  await expect(page.locator("[data-rest-status]")).toContainText("200");
});

test("JSON panels expose semantic tokens, copy exact content, and contain narrow overflow", async ({ page, context }) => {
  await context.grantPermissions(["clipboard-read", "clipboard-write"], { origin: "http://127.0.0.1:8080" });
  await page.setViewportSize({ width: 360, height: 740 });
  await page.goto("/en/rest");

  const request = page.locator("[data-rest-request]");
  await expect(request.locator(".json-token--key")).toContainText(["method", "path"]);
  await page.getByRole("button", { name: "Copy JSON" }).first().click();
  await expect(page.getByRole("button", { name: "Copied" })).toBeVisible();
  expect(await page.evaluate(() => navigator.clipboard.readText())).toBe(await request.textContent());
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= document.documentElement.clientWidth)).toBe(true);
  await expect(request).toHaveCSS("overflow-x", "auto");
});

test("home remains within a narrow viewport", async ({ page }) => {
  await page.setViewportSize({ width: 360, height: 740 });
  await page.goto("/pt-BR/");
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= document.documentElement.clientWidth)).toBe(true);
  await expect(page.locator("main")).toBeVisible();
});

test("home places database diagnostics below transport smoke tests", async ({ page }) => {
  await page.goto("/en/");
  await expect(page.locator("main > .protocol-overview h2")).toHaveText([
    "Transport smoke tests",
    "Database diagnostics",
  ]);
});

test("PostgreSQL keeps secrets out of the effective-target summary and clears them", async ({ page }) => {
  await page.goto("/en/postgres");
  await page.getByLabel("Deployment access token").fill("browser-test-token");
  await page.getByLabel("Host").fill("203.0.113.10");
  await page.getByLabel("User").fill("operator");
  await page.getByLabel("Credential secret", { exact: true }).fill("sentinel-password");
  await expect(page.locator("[data-postgres-credential-secret] .input-group")).toHaveCSS("outline-style", "solid");
  await expect(page.getByLabel("Credential secret", { exact: true })).toHaveCSS("outline-style", "none");
  await expect(page.getByLabel("Credential secret", { exact: true })).toHaveAttribute("type", "password");
  await page.getByRole("button", { name: "Show credential secret" }).click();
  await expect(page.getByLabel("Credential secret", { exact: true })).toHaveAttribute("type", "text");
  await page.getByRole("button", { name: "Hide credential secret" }).click();
  await expect(page.getByLabel("Credential secret", { exact: true })).toHaveAttribute("type", "password");
  await expect(page.locator("[data-postgres-summary]")).toContainText("203.0.113.10:5432");
  await expect(page.locator("[data-postgres-summary]")).not.toContainText("sentinel-password");
  await page.getByRole("button", { name: "Clear credentials and result" }).click();
  await expect(page.getByLabel("Deployment access token")).toHaveValue("");
  await expect(page.getByLabel("Credential secret", { exact: true })).toHaveValue("");
});

test("PostgreSQL reveals fields from capabilities and drives retained lifecycle", async ({ page }) => {
  await page.route("**/api/diagnostics/postgres/connections", async (route) => {
    if (route.request().method() !== "POST") return route.continue();
    const body = route.request().postDataJSON();
    expect(body.connection.database).toBeUndefined();
    expect(body.connection.credential.type).toBe("none");
    expect(route.request().headers()["x-testkit-correlation-id"]).toMatch(/^[0-9a-f-]{36}$/);
    await route.fulfill({
      status: 201,
      contentType: "application/json",
      headers: {
        "Testkit-Version": "v0.9.0",
        "X-Testkit-Correlation-ID": "018f47de-1234-7abc-8def-0123456789ab",
      },
      body: JSON.stringify({
        connection: { id: "retained-1", state: "ready" },
        result: { status: "success", code: "ok", duration_ms: 7.5, data: { backend_pid: 42 } },
      }),
    });
  });
  await page.route("**/api/diagnostics/postgres/connections/retained-1", (route) => route.fulfill({ status: 204 }));

  await page.goto("/en/postgres");
  await page.getByLabel("Deployment access token").fill("browser-test-token");
  await page.getByLabel("Host").fill("203.0.113.10");
  await page.getByLabel("User").fill("operator");
  await page.getByLabel("Credential type").selectOption("none");
  await expect(page.getByLabel("Credential secret", { exact: true })).toBeHidden();
  await page.getByLabel("TLS").selectOption("disable");
  await expect(page.getByLabel("Private server CA (PEM, optional)")).toBeHidden();
  await page.getByLabel("Connection lifecycle").selectOption("retained");
  await expect(page.getByRole("button", { name: "Test connection" })).toBeDisabled();
  await page.getByRole("button", { name: "Create retained connection" }).click();
  await expect(page.locator("[data-postgres-response]")).toContainText("backend_pid");
  await expect(page.locator("[data-postgres-total-duration]")).toHaveText(/^\d+ ms$/);
  await expect(page.locator("[data-postgres-diagnostic-duration]")).toHaveText("7.5 ms");
  await expect(page.locator("[data-postgres-version]")).toHaveText("v0.9.0");
  await expect(page.locator("[data-postgres-correlation-id]")).toHaveText("018f47de-1234-7abc-8def-0123456789ab");
  await expect(page.getByRole("button", { name: "Test connection" })).toBeEnabled();
  await page.getByRole("button", { name: "Destroy retained connection" }).click();
  await expect(page.locator("[data-postgres-status]")).toHaveText("Connection destroyed");
});
