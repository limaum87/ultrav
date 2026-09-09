#!/bin/sh
# Regenerates all contract-derived artifacts from docs/api/openapi.yaml
# (single source of truth):
#   1. backend Go types   (oapi-codegen)
#   2. frontend TS schema (openapi-typescript)
#   3. embedded openapi.json served by the backend
#
# Requirements: go 1.23+, node 18+, python3 (for yaml->json), run from repo root.
set -e

SPEC=docs/api/openapi.yaml

echo "==> backend Go types (oapi-codegen)"
(cd backend && go run github.com/oapi-codegen/oapi-codegen/v2/cmd/oapi-codegen@v2.4.1 -config oapi.cfg.yaml ../$SPEC)

echo "==> frontend TS schema (openapi-typescript)"
(cd frontend && npx openapi-typescript ../$SPEC -o src/api/schema.d.ts)

echo "==> embedded openapi.json"
python3 -c "
import json, yaml
d = yaml.safe_load(open('$SPEC'))
json.dump(d, open('backend/internal/api/openapi.json', 'w'), indent=2)
"

echo "Done. Remember to rebuild: docker compose -f deploy/docker-compose.yml build"
