# Wiring the portal (UI Platform track)

The portal is a separate single-page app (`ui/`) that talks to the wtc query
API. It is **additive**: the Go binary is still the whole backend, and the
zero-dependency embedded timeline (served at `/`) stays available. Run the
portal only if you want the richer dashboards/views.

Two moving parts:

- **wtc** — the Go server, serving `/api/v1/*` (bearer-authed) + `/api/openapi.json`.
- **wtc-ui** — an nginx container serving the built SPA. The browser calls the
  wtc API **directly**, cross-origin, so wtc must allow the SPA's origin via
  CORS.

```
   browser ── GET http://ui-host/            ──▶  wtc-ui (nginx, static assets)
   browser ── GET http://api-host/api/v1/... ──▶  wtc    (data; needs CORS allow)
```

## 1. Two knobs to keep consistent

| Setting | Where | What it is |
|---|---|---|
| `WTC_API_BASE_URL` | wtc-ui container env | The API URL the **browser** calls. Injected into the SPA at container start (one image, any server). Empty = same origin. |
| `server.cors.allowed_origins` | wtc config | The **browser origin** the SPA is served from. wtc echoes it on `Access-Control-Allow-Origin`; anything else gets no CORS headers and the browser blocks the call. |

They point at each other: `WTC_API_BASE_URL` is where the SPA sends requests;
`allowed_origins` is where those requests come *from*.

## 2. Docker Compose (both containers)

`deploy/compose/docker-compose.yaml` already wires both. From `deploy/compose/`:

```sh
export WTC_API_TOKEN=$(openssl rand -hex 24)
docker compose up -d --build
```

Defaults: the SPA is served at <http://localhost:8080> and calls the API at
`http://localhost:8484`; the wtc service allows the `http://localhost:8080`
origin (`WTC_SERVER_CORS_ALLOWED_ORIGINS`). Open <http://localhost:8080>, sign
in with your `WTC_API_TOKEN`.

Override the hosts for a real deployment:

```sh
# SPA reachable at https://wtc.example.com, API at https://wtc-api.example.com
WTC_API_BASE_URL=https://wtc-api.example.com \
WTC_UI_ORIGIN=https://wtc.example.com \
docker compose up -d
```

## 3. Helm (in-cluster)

The chart deploys the portal **by default** (`ui.enabled: true`) — a `wtc-ui`
Deployment + Service alongside the `wtc` API. It also ships an **opt-in
single-host Ingress** that serves the SPA at `/` and proxies `/api` to the API
on the **same origin**, so no CORS is needed and `apiBaseUrl` stays empty:

```yaml
# values.yaml — the recommended same-origin setup
ingress:
  enabled: true
  className: nginx
  host: wtc.example.com
# ui.apiBaseUrl: ""   # default; same origin via the ingress above
```

```sh
helm upgrade --install wtc deploy/helm/wtc -f values.yaml
```

Open `https://wtc.example.com` and sign in with an `auth.api_tokens` value. The
Ingress routes `/api/*` to the `wtc` Service (8484) and everything else to the
`wtc-ui` Service (80). (With `ui.enabled: false`, the same Ingress routes
everything to `wtc`, which serves the embedded timeline + API.)

### Cross-origin alternative (CORS)

To serve the SPA on a **different** origin from the API (e.g. two Ingress hosts,
or your own routing), set `ui.apiBaseUrl` to the API's URL and add the SPA's
origin to the server's CORS allow-list:

```yaml
ui:
  apiBaseUrl: https://wtc-api.example.com    # what the browser calls
config:
  server:
    cors:
      allowed_origins:
        - https://wtc-portal.example.com     # where the SPA is served
```

To skip the portal entirely, set `ui.enabled: false`.

## 4. Auth

v1 has no user login — the API token *is* the credential. The login screen
sends the token to `GET /api/v1/auth/verify`; on `200` it is stored in the
browser (`localStorage`) and sent as a bearer header on every call. Log out
clears it. Use a value from the server's `auth.api_tokens`.

## 5. Local development

Run the API from source and the SPA dev server against it:

```sh
make run                       # wtc serve with dev/wtc.yaml (allows :5173 origin)
cd ui && npm install && npm run dev   # http://localhost:5173
```

`dev/wtc.yaml` already allows `http://localhost:5173` and defines a `dev-token`.
The SPA's dev API URL is `http://localhost:8484` (`ui/public/config.js`).

## Troubleshooting

- **Login says "Could not reach the API / CORS":** the SPA origin is not in
  `server.cors.allowed_origins`, or `WTC_API_BASE_URL` points at the wrong
  host. Both must be set and consistent (§1). Confirm with:
  `curl -i -H 'Origin: <spa-origin>' -H 'Authorization: Bearer <token>' <api>/api/v1/auth/verify`
  — a correct setup returns `200` with an `Access-Control-Allow-Origin` header.
- **Token rejected (401):** it is not in `auth.api_tokens`.
- **Blank page after a deploy:** a stale `config.js`/`index.html` cache — both
  are served `no-store`, but check any CDN in front of the SPA.
