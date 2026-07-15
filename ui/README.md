# wtc portal (`ui/`)

The rich SPA client of the wtc query API (UI Platform track, P7–P10). Built and
deployed independently of the Go binary — its own toolchain, never touches
`go build`. The Go server stays the single backend; this is a client of
`/api/v1/*`.

## Stack

React 18 · TypeScript · Vite · Tailwind + shadcn-style components · TanStack
Query · React Router · a typed client generated from the server's OpenAPI spec.

## Develop

```sh
npm install
npm run gen:api        # regenerate src/api/schema.ts from ../internal/server/openapi.json
npm run dev            # http://localhost:5173
```

The dev server reads its API base URL from `public/config.js`
(`window.__WTC_CONFIG__.apiBaseUrl`, default `http://localhost:8484`). Point a
local `wtc serve` at that address and add the dev origin to the server's CORS
allow-list:

```yaml
server:
  cors:
    allowed_origins:
      - "http://localhost:5173"
```

Sign in with any value from the server's `auth.api_tokens`.

## Scripts

| Script | Does |
|---|---|
| `npm run dev` | Vite dev server |
| `npm run build` | typecheck (`tsc -b`) + production build to `dist/` |
| `npm run typecheck` | types only, no emit |
| `npm run lint` | ESLint |
| `npm run gen:api` | regenerate the typed client from the OpenAPI spec |

`src/api/schema.ts` is generated and committed so `build` is hermetic (no
running server needed). Rerun `gen:api` whenever the API contract changes.

## Deploy

`ui/Dockerfile` builds static assets served by nginx. The API base URL is
injected at container start from `WTC_API_BASE_URL` (runtime, not build time —
one image works anywhere). See `docs/setup/portal.md`.
