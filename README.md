# UltraV

Plataforma moderna, simples e **API-first** para gerenciamento de máquinas virtuais **KVM/QEMU** em hosts Linux únicos (pequeno e médio porte).

UltraV **não** é um hypervisor, nem um Proxmox, nem um OpenStack. É uma camada moderna de gerenciamento sobre tecnologias maduras e comprovadas:

```
KVM / QEMU
    │
 libvirt
    │
Domain/Core
    │
 REST API
    │
OpenAPI
 ┌────┼────┐
 │    │    │
UI   CLI  Agents
           │
        MCP/etc.
```

## Princípios

1. **Everything is an API.** Toda funcionalidade da UI existe primeiro na API. A UI nunca terá um recurso que a API não expõe.
2. **API contract-first.** O contrato OpenAPI 3.1 é a fonte da verdade (`GET /openapi.json`, Swagger UI em `GET /docs`).
3. **Monólito modular.** Sem Kubernetes, sem microservices, sem abstrações sem necessidade concreta.
4. **Agent-friendly.** Operações semanticamente específicas (nunca `POST /execute`), autorizáveis individualmente, seguras para agentes de IA no futuro.
5. **Simplicidade > features.** Fatias verticais pequenas e bem construídas.

## Stack

| Camada      | Tecnologia                                  |
|-------------|---------------------------------------------|
| Backend     | Go, REST API, SQLite                        |
| Hipervisor  | KVM, QEMU, libvirt, QEMU Guest Agent        |
| Contrato    | OpenAPI 3.1 + Swagger UI                    |
| Frontend    | React + TypeScript + Vite                   |
| Target      | Ubuntu Server / Debian                      |

## Status

**Fase 1 — Foundation** em andamento. Veja [ROADMAP.md](ROADMAP.md) e [ARCHITECTURE.md](ARCHITECTURE.md).

A primeira milestone entrega: backend Go + `MockHypervisorProvider` + REST API (`/api/v1`) + OpenAPI/Swagger + frontend (Dashboard, Virtual Machines, VM Details), tudo executável via Docker Compose sem precisar de um host KVM real.

## Estrutura (monorepo)

```
/
├── backend/           # API Go (monólito modular)
├── frontend/          # UI React + TypeScript + Vite
├── docs/              # Contrato OpenAPI, decisões, design docs
│   └── api/openapi.yaml
├── deploy/            # Docker Compose e artefatos de deploy
├── scripts/           # Automação de desenvolvimento
├── README.md
├── ROADMAP.md
└── ARCHITECTURE.md
```

## Como rodar (após implementação)

```bash
# Tudo via Docker (backend mock + frontend)
docker compose -f deploy/docker-compose.yml up --build

# UI:      http://localhost:8275
# API:     http://localhost:8275/api/v1/...
# Swagger: http://localhost:8275/docs
```

## Licença

A definir (sugestão: Apache-2.0 ou MIT).
