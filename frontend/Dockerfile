# Build context is the REPOSITORY ROOT:
#   docker build -f frontend/Dockerfile .
# The typed API client is regenerated from the contract so the UI can never
# drift from docs/api/openapi.yaml.
FROM node:22-alpine AS build
WORKDIR /src
COPY frontend/package.json ./
RUN npm install --include=dev --no-audit --no-fund
COPY frontend/ .
COPY docs/api/openapi.yaml /contract/openapi.yaml
RUN npx openapi-typescript /contract/openapi.yaml -o src/api/schema.d.ts && npm run build

# nginx serves the SPA and proxies /api, /openapi.json and /docs to the
# backend — a single exposed port, no CORS in the deployed setup.
FROM nginx:1.27-alpine
COPY --from=build /src/dist /usr/share/nginx/html
COPY frontend/nginx.conf /etc/nginx/conf.d/default.conf
# noVNC requires a secure context: serve TLS with a self-signed certificate
# generated at build time (single-host internal deployment).
# ULTRAV_HOST_IP is the address the UI is reached at; without it in the SAN the
# browser rejects https://<host-ip>:8275 with a name mismatch (the compose file
# passes it from deploy/.env).
ARG ULTRAV_HOST_IP=127.0.0.1
RUN apk add --no-cache openssl && mkdir -p /etc/nginx/certs && \
    openssl req -x509 -newkey rsa:2048 -sha256 -days 3650 -nodes \
      -keyout /etc/nginx/certs/tls.key -out /etc/nginx/certs/tls.crt \
      -subj "/CN=ultrav" \
      -addext "subjectAltName=DNS:ultrav,DNS:localhost,IP:127.0.0.1,IP:${ULTRAV_HOST_IP}"
EXPOSE 80 443
