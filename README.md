# UltraV

<p align="center">
  <em>Modern KVM/QEMU virtual machine management — simple, API-first and agent-friendly.</em>
</p>

UltraV is a platform for managing **KVM/QEMU** virtual machines on single Linux hosts (small and medium scale), through a REST API described by OpenAPI 3.1 and a React web UI.

> **Important:** UltraV is **not** a hypervisor, nor a Proxmox, nor an OpenStack. It is a thin management layer on top of proven technologies:
>
> ```
> KVM / QEMU  →  libvirt  →  UltraV API  →  UI / CLI / Automation / AI Agents
> ```

## ✨ Principles

1. **Everything is an API** — every UI feature exists in the API first; the UI is just another client.
2. **Contract-first** — the OpenAPI 3.1 contract (`docs/api/openapi.yaml`) is the source of truth; Go types and the TypeScript client are generated from it.
3. **Modular monolith** — no Kubernetes, no microservices, no abstractions without a concrete need.
4. **Agent-friendly** — semantically specific operations (never `POST /execute`), ready for fine-grained authorization and safe use by AI agents in the future.
5. **Simplicity > features** — small, well-built vertical slices.

## 🧰 Stack

| Layer      | Technology                          |
|------------|-------------------------------------|
| Backend    | Go, `net/http` std, REST API        |
| Hypervisor | KVM, QEMU, libvirt, QEMU Guest Agent|
| Contract   | OpenAPI 3.1 + Swagger UI            |
| Frontend   | React + TypeScript + Vite           |
| Deploy     | Docker / Docker Compose, nginx      |
| Target     | Ubuntu Server / Debian              |

## 🚀 Quick start (Docker)

No KVM host required — the backend runs with `MockHypervisorProvider` (realistic fake VMs):

```bash
docker compose -f deploy/docker-compose.yml up --build
```

| Service  | URL                                |
|----------|------------------------------------|
| UI       | http://localhost:8275              |
| Swagger  | http://localhost:8275/docs         |
| API      | http://localhost:8275/api/v1/...   |

Deployment details: [`deploy/README.md`](deploy/README.md).

## 🖥️ Running against a real KVM host

On the host (Ubuntu 22.04+ / Debian 12):

```bash
sudo apt install libvirt-daemon-system qemu-kvm
HYPERVISOR_PROVIDER=libvirt \
HYPERVISOR_LIBVIRT_URI=qemu:///system \
ULTRAV_PORT=8275 ./ultrav
```

The libvirt provider uses the `libvirt_dlopen` build tag: `libvirt.so` is loaded at runtime, so **the same binary** runs in mock mode or against real libvirt.

## 🧑‍💻 Local development (no Docker)

```bash
# Backend (mock mode)
cd backend && go run ./cmd/ultrav

# Frontend (dev server with /api proxy)
cd frontend && npm install && npm run dev
```

Regenerate types/client from the OpenAPI contract:

```bash
./scripts/generate.sh
```

Generated artifacts are versioned in the repo; the frontend Docker build **regenerates** the client, ensuring the UI never compiles against a stale contract.

## 📡 API

Prefix `/api/v1` · Contract: `GET /openapi.json` · Swagger UI: `GET /docs`

| Method | Path                                | Description                     |
|--------|-------------------------------------|---------------------------------|
| GET    | `/api/v1/host`                      | Host information                |
| GET    | `/api/v1/capabilities`              | Hypervisor capabilities         |
| GET    | `/api/v1/vms`                       | List VMs                        |
| GET    | `/api/v1/vms/{id}`                  | VM details                      |
| POST   | `/api/v1/vms/{id}/start`            | Start VM                        |
| POST   | `/api/v1/vms/{id}/shutdown`         | Graceful shutdown (ACPI)        |
| POST   | `/api/v1/vms/{id}/reboot`           | Reboot VM                       |
| POST   | `/api/v1/vms/{id}/stop`             | Force stop ("pull the plug")    |
| GET    | `/api/v1/storage/pools`             | Storage pools                   |
| POST   | `/api/v1/storage/pools/{id}/refresh`| Refresh pool                    |
| GET    | `/api/v1/networks`                  | Virtual networks                |
| POST   | `/api/v1/networks/{id}/start|stop`  | Start/stop network              |
| GET    | `/api/v1/health` · `/api/v1/ready`  | Liveness / readiness            |

**Stable error model** in every response:

```json
{
  "error": {
    "code": "VM_NOT_FOUND",
    "message": "Virtual machine was not found",
    "requestId": "req_01HV3M9Z2K"
  }
}
```

## 🏗️ Architecture

```
KVM / QEMU + QEMU Guest Agent
        │
     libvirt
        │
  HypervisorProvider   ← the only boundary with the hypervisor
        │
    Domain / Core
        │
      REST API (/api/v1)
        │
     OpenAPI 3.1
   ┌────┼────┬──────────┐
  UI   CLI  Automation  AI Agents (future, via MCP)
```

- **`HypervisorProvider`** is the only boundary with libvirt. Two implementations: `mock` (dev/tests) and `libvirt` (production), selected via env var — only `main` knows the concrete implementations.
- **Codegen**: `oapi-codegen` (Go types) and `openapi-typescript` + `openapi-fetch` (TS client). No parallel DTOs, no manual `fetch`.
- **Security**: no shell, no concatenated commands, input validation, error model without stack traces, correlation ID (`X-Request-Id`) on every request, JSON structured logging.

More details: [`ARCHITECTURE.md`](ARCHITECTURE.md).

## 📁 Repository layout (monorepo)

```
/
├── backend/           # Go API (modular monolith)
├── frontend/          # React + TypeScript + Vite UI
├── docs/              # OpenAPI contract and design docs
├── deploy/            # Docker Compose and deployment artifacts
├── scripts/           # Development automation (codegen)
├── ROADMAP.md         # Phases and current milestone
└── ARCHITECTURE.md    # Architectural decisions
```

## 🗺️ Status and roadmap

**Phase 1 — Foundation**: Go backend + `MockHypervisorProvider` + REST API + OpenAPI/Swagger + frontend (Dashboard, Virtual Machines, VM Details), all runnable via Docker Compose.

Upcoming phases (see [`ROADMAP.md`](ROADMAP.md)): real libvirt provider, Job System for async operations, incremental backups with QEMU dirty bitmaps, multi-host and MCP adapter for AI agents.

## 🤖 Installing this system

To install UltraV on a host — manually or by delegating to an AI agent — follow the step-by-step guide in [`AGENTS.md`](AGENTS.md). It covers environment checks, mock mode, real KVM/libvirt setup, validation checklists and troubleshooting.

## 📄 License

To be decided (suggestion: Apache-2.0 or MIT).
