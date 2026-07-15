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

`deploy/docker-compose.yaml` already wires both. From `deploy/`:

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

The chart ships the portal as an opt-in component (`ui.enabled=false` by
default). Enable it and tell both sides about each other:

```yaml
# values.yaml
ui:
  enabled: true
  apiBaseUrl: https://wtc-api.example.com   # what the browser calls

config:
  server:
    cors:
      allowed_origins:
        - https://wtc-portal.example.com    # where the SPA is served
```

```sh
helm upgrade --install wtc deploy/helm/wtc -f values.yaml
```

This adds a `wtc-ui` Deployment + Service (port 80). Front it with your own
Ingress. Point an Ingress host at the `wtc-ui` Service for the SPA and at the
`wtc` Service (8484) for the API.

### Same-origin alternative (no CORS)

If you put the SPA and the API behind **one** Ingress host — SPA at `/`, API
proxied at `/api` — set `ui.apiBaseUrl: ""` (same origin) and leave
`cors.allowed_origins` empty. The browser then makes same-origin calls and no
CORS config is needed. (The bundled compose/Helm defaults use the direct
cross-origin path instead, which is what the CORS setting is for.)

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
