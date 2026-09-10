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

test("home remains within a narrow viewport", async ({ page }) => {
  await page.setViewportSize({ width: 360, height: 740 });
  await page.goto("/pt-BR/");
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= document.documentElement.clientWidth)).toBe(true);
  await expect(page.locator("main")).toBeVisible();
});

test("PostgreSQL keeps secrets out of the effective-target summary and clears them", async ({ page }) => {
  await page.goto("/en/postgres");
  await page.getByLabel("Deployment access token").fill("browser-test-token");
  await page.getByLabel("Host").fill("203.0.113.10");
  await page.getByLabel("User").fill("operator");
  await page.getByLabel("Password").fill("sentinel-password");
  await expect(page.locator("[data-postgres-summary]")).toContainText("203.0.113.10:5432");
  await expect(page.locator("[data-postgres-summary]")).not.toContainText("sentinel-password");
  await page.getByRole("button", { name: "Clear credentials and result" }).click();
  await expect(page.getByLabel("Deployment access token")).toHaveValue("");
  await expect(page.getByLabel("Password")).toHaveValue("");
});
