# Deploy — Docker Compose

Sobe a aplicação completa **em modo libvirt**, contra o daemon do host. Para
demonstração sem KVM, use `HYPERVISOR_PROVIDER=mock` no `deploy/.env`.

```bash
cp deploy/.env.example deploy/.env
# preencha LIBVIRT_GID e KVM_GID (getent group libvirt kvm), ULTRAV_HOST_IP
# e ULTRAV_JWT_SECRET (openssl rand -hex 32)
docker compose -f deploy/docker-compose.yml up -d --build

# UI:      https://localhost:8275   (certificado autoassinado)
# Swagger: https://localhost:8275/docs
# API:     https://localhost:8275/api/v1/...
```

## Como funciona

- **Porta única (8275 → 443).** O container `frontend` (nginx) serve a SPA em **HTTPS** (noVNC exige contexto seguro) e faz proxy de `/api`, `/openapi.json` e `/docs` para o `backend:8080`. Sem CORS no deploy. O SAN do certificado é gerado a partir de `ULTRAV_HOST_IP`.
- **Parâmetros de host em `deploy/.env`**: GIDs para `group_add`, IP do certificado e segredo JWT são obrigatórios — sem eles o Compose aborta com mensagem explícita (`defina LIBVIRT_GID em deploy/.env`) em vez de subir com valor errado.
- **Backend**: build multi-stage (`backend/Dockerfile`); o estágio `test` roda `go test ./...` durante o build. Runtime alpine, usuário não-root, healthcheck em `/api/v1/health`.
- **Frontend**: build multi-stage (`frontend/Dockerfile`); o client TypeScript é **regenerado a partir de `docs/api/openapi.yaml`** dentro do build — a UI nunca diverge do contrato.
- **Healthcheck do compose** usa `GET /api/v1/health`; o frontend só inicia com o backend healthy.

## Rodar contra um host KVM real

No host (Ubuntu 22.04+/Debian 12): instale `libvirt-daemon-system qemu-kvm` e copie o binário do backend (ou rode o container montando o socket do libvirt):

```bash
HYPERVISOR_PROVIDER=libvirt \
HYPERVISOR_LIBVIRT_URI=qemu:///system \
ULTRAV_PORT=8275 ./ultrav

# ou via container, montando o socket do daemon:
docker run --rm -p 8275:8080 -v /var/run/libvirt:/var/run/libvirt ultrav-backend \
  # envs acima via -e
```

O frontend continua sendo só o `deploy/docker-compose.yml` (serviço `frontend`) apontando para o backend do host.

### Biblioteca de ISOs com backend em container

O backend grava o caminho do ISO no XML do domínio, mas quem abre o arquivo é o **qemu, no host**. Por isso, em modo libvirt o diretório de ISOs precisa ser um bind mount de **caminho idêntico** nos dois lados — um volume nomeado faz o upload funcionar e a VM falhar só no start, com `Cannot access storage file`. O compose já monta ISOs, imagens de disco e backups dessa forma; se você mudar esses mounts, mantenha a regra:

```yaml
services:
  backend:
    environment:
      ULTRAV_ISO_DIR: /var/lib/libvirt/isos
    volumes:
      - /var/lib/libvirt/isos:/var/lib/libvirt/isos
```

O diretório precisa ser gravável pelo usuário do container (`nobody`, que entra no grupo `kvm` via `group_add`):

```bash
sudo chgrp kvm /var/lib/libvirt/isos && sudo chmod 2775 /var/lib/libvirt/isos
```

A mesma regra vale para qualquer diretório cujo caminho acabe no XML do domínio (pools de disco, por exemplo).

## Desenvolvimento sem Docker

```bash
# backend (requer Go 1.23+)
cd backend && HYPERVISOR_PROVIDER=mock ULTRAV_PORT=8080 go run ./cmd/ultrav

# frontend (proxy /api -> :8080 já configurado no vite.config.ts)
cd frontend && npm install --include=dev && npm run dev   # http://localhost:5173
```

## Regenerar artefatos do contrato

Após alterar `docs/api/openapi.yaml`:

```bash
./scripts/generate.sh
```

Gera: tipos Go (oapi-codegen), schema TypeScript (openapi-typescript) e o `openapi.json` embutido no binário.
