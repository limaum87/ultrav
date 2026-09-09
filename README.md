# UltraV

<p align="center">
  <em>Gerenciamento moderno de VMs KVM/QEMU — simples, API-first e agent-friendly.</em>
</p>

UltraV é uma plataforma para gerenciar máquinas virtuais **KVM/QEMU** em hosts Linux únicos (pequeno e médio porte), via uma REST API descrita por OpenAPI 3.1 e uma web UI React.

> **Importante:** UltraV **não** é um hypervisor, nem um Proxmox, nem um OpenStack. É uma camada fina de gerenciamento sobre tecnologias maduras:
>
> ```
> KVM / QEMU  →  libvirt  →  UltraV API  →  UI / CLI / Automação / AI Agents
> ```

## ✨ Princípios

1. **Everything is an API** — toda funcionalidade da UI existe primeiro na API; a UI é apenas mais um cliente.
2. **Contract-first** — o contrato OpenAPI 3.1 (`docs/api/openapi.yaml`) é a fonte da verdade; tipos Go e o client TypeScript são gerados dele.
3. **Monólito modular** — sem Kubernetes, sem microservices, sem abstrações sem necessidade concreta.
4. **Agent-friendly** — operações semanticamente específicas (nunca `POST /execute`), prontas para autorização granular e uso seguro por agentes de IA no futuro.
5. **Simplicidade > features** — fatias verticais pequenas e bem construídas.

## 🧰 Stack

| Camada     | Tecnologia                          |
|------------|-------------------------------------|
| Backend    | Go, `net/http` std, REST API        |
| Hipervisor | KVM, QEMU, libvirt, QEMU Guest Agent|
| Contrato   | OpenAPI 3.1 + Swagger UI            |
| Frontend   | React + TypeScript + Vite           |
| Deploy     | Docker / Docker Compose, nginx      |
| Target     | Ubuntu Server / Debian              |

## 🚀 Quick start (Docker)

Nenhum host KVM é necessário — o backend roda com `MockHypervisorProvider` (VMs fictícias realistas):

```bash
docker compose -f deploy/docker-compose.yml up --build
```

| Serviço  | URL                                |
|----------|------------------------------------|
| UI       | http://localhost:8275              |
| Swagger  | http://localhost:8275/docs         |
| API      | http://localhost:8275/api/v1/...   |

Detalhes do deploy: [`deploy/README.md`](deploy/README.md).

## 🖥️ Rodar contra um host KVM real

No host (Ubuntu 22.04+ / Debian 12):

```bash
sudo apt install libvirt-daemon-system qemu-kvm
HYPERVISOR_PROVIDER=libvirt \
HYPERVISOR_LIBVIRT_URI=qemu:///system \
ULTRAV_PORT=8275 ./ultrav
```

O provider libvirt usa build tag `libvirt_dlopen`: a `libvirt.so` é carregada em runtime, então **o mesmo binário** roda em modo mock ou com libvirt real.

## 🧑‍💻 Desenvolvimento local (sem Docker)

```bash
# Backend (modo mock)
cd backend && go run ./cmd/ultrav

# Frontend (dev server com proxy /api)
cd frontend && npm install && npm run dev
```

Regenerar tipos/client a partir do contrato OpenAPI:

```bash
./scripts/generate.sh
```

Os artefatos gerados são versionados no repo; o build Docker do frontend **regenera** o client, garantindo que a UI nunca compila contra um contrato antigo.

## 📡 API

Prefixo `/api/v1` · Contrato: `GET /openapi.json` · Swagger UI: `GET /docs`

| Método | Path                                | Descrição                       |
|--------|-------------------------------------|---------------------------------|
| GET    | `/api/v1/host`                      | Informações do host             |
| GET    | `/api/v1/capabilities`              | Capacidades do hipervisor       |
| GET    | `/api/v1/vms`                       | Lista de VMs                    |
| GET    | `/api/v1/vms/{id}`                  | Detalhes de uma VM              |
| POST   | `/api/v1/vms/{id}/start`            | Ligar VM                        |
| POST   | `/api/v1/vms/{id}/shutdown`         | Shutdown graceful (ACPI)        |
| POST   | `/api/v1/vms/{id}/reboot`           | Reiniciar VM                    |
| POST   | `/api/v1/vms/{id}/stop`             | Force stop ("puxar o cabo")     |
| GET    | `/api/v1/storage/pools`             | Pools de storage                |
| POST   | `/api/v1/storage/pools/{id}/refresh`| Refresh de pool                 |
| GET    | `/api/v1/networks`                  | Redes virtuais                  |
| POST   | `/api/v1/networks/{id}/start|stop`  | Ligar/desligar rede             |
| GET    | `/api/v1/health` · `/api/v1/ready`  | Liveness / readiness            |

**Error model** estável em toda resposta:

```json
{
  "error": {
    "code": "VM_NOT_FOUND",
    "message": "Virtual machine was not found",
    "requestId": "req_01HV3M9Z2K"
  }
}
```

## 🏗️ Arquitetura

```
KVM / QEMU + QEMU Guest Agent
        │
     libvirt
        │
  HypervisorProvider   ← única fronteira com o hipervisor
        │
    Domain / Core
        │
      REST API (/api/v1)
        │
     OpenAPI 3.1
   ┌────┼────┬──────────┐
  UI   CLI  Automação  AI Agents (futuro, via MCP)
```

- **`HypervisorProvider`** é a única fronteira com o libvirt. Duas implementações: `mock` (dev/testes) e `libvirt` (produção), escolhidas por env var — só o `main` conhece implementações concretas.
- **Codegen**: `oapi-codegen` (tipos Go) e `openapi-typescript` + `openapi-fetch` (client TS). Sem DTOs paralelos, sem `fetch` manual.
- **Segurança**: sem shell, sem comandos concatenados, validação de inputs, error model sem stack traces, correlation ID (`X-Request-Id`) em toda request, structured logging JSON.

Mais detalhes: [`ARCHITECTURE.md`](ARCHITECTURE.md).

## 📁 Estrutura (monorepo)

```
/
├── backend/           # API Go (monólito modular)
├── frontend/          # UI React + TypeScript + Vite
├── docs/              # Contrato OpenAPI e design docs
├── deploy/            # Docker Compose e artefatos de deploy
├── scripts/           # Automação de desenvolvimento (codegen)
├── ROADMAP.md         # Fases e milestone atual
└── ARCHITECTURE.md    # Decisões arquiteturais
```

## 🗺️ Status e roadmap

**Fase 1 — Foundation**: backend Go + `MockHypervisorProvider` + REST API + OpenAPI/Swagger + frontend (Dashboard, Virtual Machines, VM Details), tudo executável via Docker Compose.

Próximas fases (ver [`ROADMAP.md`](ROADMAP.md)): provider libvirt real, Job System para operações assíncronas, backups incrementais com QEMU dirty bitmaps, multi-host e adapter MCP para agentes de IA.

## 📄 Licença

A definir (sugestão: Apache-2.0 ou MIT).
