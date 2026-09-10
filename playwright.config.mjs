import { defineConfig } from "@playwright/test";
import { fileURLToPath } from "node:url";

const tokenFile = fileURLToPath(new URL("./frontend-tests/fixtures/diagnostic-token", import.meta.url));
const policyFile = fileURLToPath(new URL("./frontend-tests/fixtures/postgres-destinations.json", import.meta.url));

export default defineConfig({
  testDir: "frontend-tests/browser",
  timeout: 15_000,
  use: { baseURL: "http://127.0.0.1:8080", browserName: "chromium" },
  webServer: {
    command: "go run .",
    url: "http://127.0.0.1:8080/healthz",
    reuseExistingServer: false,
    timeout: 30_000,
    env: {
      ...process.env,
      GOCACHE: process.env.GOCACHE || "/tmp/testkit-browser-go-cache",
      HTTP_PORT: "8080",
      TESTKIT_SMOKES: "transport,postgres",
      TESTKIT_DIAGNOSTIC_TOKEN_FILE: tokenFile,
      TESTKIT_POSTGRES_DESTINATIONS_FILE: policyFile,
    },
  },
});
