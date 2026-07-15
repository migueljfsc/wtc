// Dev/default runtime config. In production the container entrypoint
// (docker-entrypoint.sh) overwrites this file from WTC_API_BASE_URL.
// An empty apiBaseUrl means "same origin" — used when a reverse proxy fronts
// both the SPA and the API.
window.__WTC_CONFIG__ = {
  apiBaseUrl: "http://localhost:8484",
};
