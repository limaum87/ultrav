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
EXPOSE 80
