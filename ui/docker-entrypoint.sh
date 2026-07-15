#!/bin/sh
# Regenerate the runtime config from WTC_API_BASE_URL on every container start,
# so one built image talks to any wtc server. Runs via nginx:alpine's
# /docker-entrypoint.d/ hook before nginx starts.
#
# Empty (the default) means "same origin" — correct when a reverse proxy fronts
# both the SPA and the API. Otherwise set it to the wtc API's public URL and add
# this SPA's origin to the server's server.cors.allowed_origins.
set -eu

: "${WTC_API_BASE_URL:=}"

cat > /usr/share/nginx/html/config.js <<EOF
window.__WTC_CONFIG__ = { apiBaseUrl: "${WTC_API_BASE_URL}" };
EOF

echo "wtc-ui: apiBaseUrl=\"${WTC_API_BASE_URL}\""
