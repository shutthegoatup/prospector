import { defineConfig, devices } from "@playwright/test";

import { base, port } from "./a11y/world.ts";

// npm run a11y: every page through axe, in Chromium, against a real server.
export default defineConfig({
  testDir: "a11y",
  forbidOnly: !!process.env.CI,
  reporter: process.env.CI ? [["list"], ["github"]] : "list",
  use: { baseURL: base, ...devices["Desktop Chrome"] },
  projects: [
    { name: "setup", testMatch: /setup\.ts$/ },
    { name: "pages", dependencies: ["setup"] },
  ],
  webServer: {
    command: "node a11y/serve.ts",
    url: base,
    env: { A11Y_PORT: String(port) },
    reuseExistingServer: false,
    // So serve.ts drops its database; the default is SIGKILL.
    gracefulShutdown: { signal: "SIGTERM", timeout: 10_000 },
    timeout: 180_000,
  },
});
