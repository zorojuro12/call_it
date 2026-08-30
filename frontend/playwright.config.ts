import { defineConfig, devices } from "@playwright/test";

export default defineConfig({
  testDir: "./e2e",
  fullyParallel: false,
  retries: 0,
  reporter: "list",
  use: {
    baseURL: "http://localhost:3000",
    trace: "on-first-retry",
  },
  projects: [
    {
      name: "chromium",
      use: { ...devices["Desktop Chrome"] },
    },
  ],
  webServer: [
    {
      command:
        'export PATH="$PATH:$HOME/.local/go/bin:$HOME/go/bin" && cd ../backend && JWT_SECRET=$(openssl rand -hex 32) CORS_ALLOWED_ORIGINS=http://localhost:3000 REDIS_ADDR=${REDIS_ADDR:-localhost:6379} go run ./cmd/api',
      url: "http://localhost:8080/healthz",
      reuseExistingServer: true,
      timeout: 60_000,
    },
    {
      command: "npm run build && npm run start",
      url: "http://localhost:3000",
      reuseExistingServer: true,
      timeout: 120_000,
      env: {
        NEXT_PUBLIC_API_BASE_URL: "http://localhost:8080",
      },
    },
  ],
});
