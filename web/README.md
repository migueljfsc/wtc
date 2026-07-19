# web/ — embedded timeline UI

The embedded timeline UI, served at `/` from the single binary. **Toolchain-free
by design:** hand-written HTML/CSS/vanilla JS, no node, no npm, no bundler. If a
change seems to need a framework, it's out of scope.

| File | What |
|---|---|
| `web.go` | `go:embed static` + `FS()` — the server mounts this at `/` |
| `static/index.html` | filter bar + timeline container; favicon is an inline data-URI (no asset fetches) |
| `static/style.css` | dark theme, status-colored rows, mobile breakpoint at 640px |
| `static/app.js` | fetches `/api/events`, day-groups, renders, cursor "load more", 60s auto-refresh; token in localStorage |

Notes:

- Served behind the Go 1.22 mux's `GET /` catch-all — every registered API
  route wins over it, so the UI can never shadow the API.
- Static files are public; **every data call is bearer-authed** (the token
  the user pastes, kept in localStorage). No session, no cookies.
- No external network at runtime — CSP-friendly, works in an air-gapped
  cluster.

Edit a file, `make build`, reload — the embed picks it up at compile time.
