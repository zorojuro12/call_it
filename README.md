# CallIt

Real-time flash-prediction / micro-wagering platform for group watch parties.

Full design and build docs live in `docs/`; binding conventions are in
[`CLAUDE.md`](CLAUDE.md). This file is a quick-start only.

## Backend

```bash
make up                                  # Redis + PostgreSQL (Kafka: make up-full)
JWT_SECRET=$(openssl rand -hex 32) go run ./backend/cmd/api
```

`JWT_SECRET` (32+ bytes) is required, no default. See `CLAUDE.md`'s Build &
Test section for the full environment variable list, including
`CORS_ALLOWED_ORIGINS` below.

### `CORS_ALLOWED_ORIGINS`

A comma-separated allowlist of browser origins permitted to call the REST
API and open the WebSocket (`internal/httpapi.CORS` and the WS upgrader's
`CheckOrigin` both read this one value — there is exactly one allowlist,
never two).

- Defaults to `http://localhost:3000` outside `ENV=production`.
- **Required** when `ENV=production` — the process fails fast without it,
  the same way it does without `JWT_SECRET`.
- Never accepts `*`, in any environment.

## Frontend

```bash
make fe-install   # npm ci
make fe-dev       # next dev, http://localhost:3000
```

### `NEXT_PUBLIC_API_BASE_URL`

The backend's base URL, e.g. `http://localhost:8080`. The only environment
value the browser bundle may carry — no secret goes in a `NEXT_PUBLIC_*`
name.

- Defaults to `http://localhost:8080` outside `NODE_ENV=production`.
- **Required** when `NODE_ENV=production` — `npm run build` and
  `npm run start` both fail fast without it (`lib/config.ts`).

Other targets: `make fe-test`, `make fe-lint`, `make fe-build`, `make fe-e2e`
(Playwright — needs the backend and Redis running; see
`frontend/playwright.config.ts`).
