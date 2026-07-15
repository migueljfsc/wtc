# wtc portal — static SPA served by nginx. Built independently of the Go binary
# (build context is ui/ only; src/api/schema.ts is committed so no running
# server is needed at build time).

# --- build ---
FROM node:22-alpine AS build
WORKDIR /app
COPY package.json package-lock.json ./
RUN npm ci
COPY . .
RUN npm run build

# --- serve ---
FROM nginx:1.27-alpine
COPY nginx.conf /etc/nginx/conf.d/default.conf
COPY docker-entrypoint.sh /docker-entrypoint.d/40-wtc-config.sh
COPY --from=build /app/dist /usr/share/nginx/html
RUN chmod +x /docker-entrypoint.d/40-wtc-config.sh
# nginx:alpine's own entrypoint runs /docker-entrypoint.d/*.sh then starts nginx,
# so the API-base-URL injection happens on every container start.
EXPOSE 80
