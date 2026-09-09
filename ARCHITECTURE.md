# UltraV — Arquitetura

## 1. Visão geral

UltraV é um **monólito modular** em Go, exposto como API REST versionada (`/api/v1`), descrita por contrato OpenAPI 3.1, com frontend React que consome **exclusivamente** essa API.

Camadas (de baixo para cima):

```
KVM / QEMU  +  QEMU Guest Agent
        │
     libvirt
        │
  HypervisorProvider   ← única fronteira com o hipervisor
        │
    Domain / Core      ← regras de negócio, estados de VM
        │
      REST API         ← handlers HTTP, validação, error model
        │
     OpenAPI           ← contrato público (fonte da verdade)
        │
 ┌─────┼──────┬─────────┐
 Web UI  CLI  Automação  AI Agents (via MCP adapter, futuro)
```

## 2. Princípios arquiteturais

1. **API-first absoluto.** A UI é apenas mais um cliente da API. Nenhuma funcionalidade existe só na UI.
2. **Contract-first.** `docs/api/openapi.yaml` define o contrato; handlers e clientes derivam dele. O backend serve `GET /openapi.json` e Swagger UI em `GET /docs`.
3. **Fronteira única com libvirt.** Nenhum handler HTTP fala com libvirt. Handlers → Domain → `HypervisorProvider`. O Domain não conhece libvirt.
4. **Desacoplamento suficiente, nada mais.** O `HypervisorProvider` existe para permitir desenvolvimento e testes sem KVM real (`MockHypervisorProvider`). Não há camadas de repositório genéricas, DTOs duplicados ou factories sem consumidor concreto.
5. **Segurança por design.** Sem shell, sem concatenação de comandos, sem path traversal, sem stack traces em respostas. Correlation ID em toda request.

## 3. HypervisorProvider

Interface pequena e explícita (nada de `Execute(action)`):

```go
type HypervisorProvider interface {
    GetHost(ctx) (Host, error)
    GetCapabilities(ctx) (Capabilities, error)
    ListVMs(ctx) ([]VM, error)
    GetVM(ctx, id string) (VM, error)
    StartVM(ctx, id string) error
    ShutdownVM(ctx, id string) error   // graceful (ACPI)
    RebootVM(ctx, id string) error
    ForceStopVM(ctx, id string) error  // equivalente a puxar o cabo
}
```

Implementações:

| Implementação               | Quando                                       |
|-----------------------------|----------------------------------------------|
| `MockHypervisorProvider`    | `HYPERVISOR_PROVIDER=mock` — desenvolvimento/testes sem KVM |
| `LibvirtHypervisorProvider` | `HYPERVISOR_PROVIDER=libvirt` — produção (`HYPERVISOR_LIBVIRT_URI`, default `qemu:///system`) |

Seleção por env var no bootstrap (`cmd/ultrav/main.go` é o único lugar que conhece implementações concretas); nada mais no sistema sabe qual está ativo. A interface inclui `Ready(ctx)` para o probe de readiness.

## 4.1 Codegen a partir do contrato

`docs/api/openapi.yaml` é a fonte da verdade e gera, via `scripts/generate.sh`:

- **Tipos Go** — [oapi-codegen](https://github.com/oapi-codegen/oapi-codegen) (somente `models`; handlers são escritos à mão com `net/http` std) → `backend/internal/api/types/gen.go`. O domínio e os providers usam esses tipos diretamente: **não existem DTOs paralelos**.
- **Schema TypeScript** — [openapi-typescript](https://openapi-typescript.com) → `frontend/src/api/schema.d.ts`, consumido pelo client tipado [openapi-fetch](https://openapi-fetch.dev). Não há `fetch` manual nem tipos duplicados.
- **`openapi.json`** — convertido de YAML e embutido no binário (`go:embed`), servido em `GET /openapi.json` com Swagger UI em `GET /docs`.

Os artefatos gerados são versionados no repo; o build Docker do frontend **regenera** o client, garantindo que a UI nunca compila contra um contrato antigo.

## 4. MockHypervisorProvider (fase 1)

Simula o host `kvm01`:

- CPU: AMD EPYC 7402P (24 cores / 48 threads, fictício)
- RAM: 64 GB
- Storage: 2 TB

VMs fictícias (mistura de estados):

| VM             | Estado     | vCPU | RAM    | Disco        | Observação          |
|----------------|-----------|------|--------|--------------|---------------------|
| `erp01`        | running   | 4    | 8 GB   | 120 GB qcow2 | IP 10.0.0.11        |
| `web01`        | running   | 2    | 4 GB   | 40 GB qcow2  | IP 10.0.0.12        |
| `database01`   | running   | 8    | 32 GB  | 500 GB qcow2 | IP 10.0.0.13        |
| `monitoring01` | stopped   | 2    | 4 GB   | 60 GB qcow2  | sem IP              |

Comportamento realista:

- `start`: stopped → running (define boot time, atribui IP do pool mock)
- `shutdown`: running → graceful shutdown (estado `shutting-down` breve → stopped)
- `reboot`: apenas se running (reinicia uptime)
- `forceStop`: running → stopped imediatamente
- CPU/RAM usados do host variam com o número de VMs ligadas; uptime das VMs cresce com o tempo

Estado **em memória** (SQLite deliberadamente fora da fase 1 — não há necessidade concreta de persistência ainda; será introduzida junto do Job System/auth).

## 5. API REST

- Prefixo: `/api/v1`
- Contrato: `docs/api/openapi.yaml` (OpenAPI 3.1), servido em `GET /openapi.json`
- Swagger UI: `GET /docs`

### Endpoints da fase 1

| Método | Path                          | operationId             |
|--------|-------------------------------|-------------------------|
| GET    | `/api/v1/storage/pools`            | `listStoragePools`      |
| GET    | `/api/v1/storage/pools/{id}`       | `getStoragePool`        |
| POST   | `/api/v1/storage/pools/{id}/refresh` | `refreshStoragePool`  |
| GET    | `/api/v1/networks`                 | `listNetworks`          |
| GET    | `/api/v1/networks/{id}`            | `getNetwork`            |
| POST   | `/api/v1/networks/{id}/start`      | `startNetwork`          |
| POST   | `/api/v1/networks/{id}/stop`       | `stopNetwork`           |
| GET    | `/api/v1/health`                   | `getHealth`             |
| GET    | `/api/v1/ready`               | `getReadiness`          |
| GET    | `/api/v1/host`                | `getHost`               |
| GET    | `/api/v1/capabilities`        | `getCapabilities`       |
| GET    | `/api/v1/vms`                 | `listVirtualMachines`   |
| GET    | `/api/v1/vms/{id}`            | `getVirtualMachine`     |
| POST   | `/api/v1/vms/{id}/start`      | `startVirtualMachine`   |
| POST   | `/api/v1/vms/{id}/shutdown`   | `shutdownVirtualMachine`|
| POST   | `/api/v1/vms/{id}/reboot`     | `rebootVirtualMachine`  |
| POST   | `/api/v1/vms/{id}/stop`       | `forceStopVirtualMachine`|

`health` verifica apenas que o processo está vivo. `ready` verifica aplicação **e** `HypervisorProvider` prontos para atender (no modo libvirt, conecta ao daemon).

### Provider libvirt (fase 2)

- Binding oficial `libvirt.org/go/libvirt` com build tag `libvirt_dlopen`: a `libvirt.so` é carregada em runtime, então o mesmo binário roda mock (sem libvirt) e libvirt real. Nenhum handler importa libvirt — só `internal/hypervisor/libvirt`.
- Estados de domínio, XML de domínios/pools/networks e QEMU Guest Agent (`ListAllInterfaceAddresses`) são traduzidos para os modelos do contrato; IPs aparecem só quando o guest agent responde.
- Em hosts reais: instalar `libvirtd` + `qemu-kvm`, rodar o binário com acesso ao socket `/var/run/libvirt/libvirt-sock`.

Proibido: endpoints genéricos tipo `POST /execute`, `runCommand`, `performOperation`. Operações semanticamente específicas são requisito de segurança para agentes de IA (cada operação pode receber escopo/autorização própria: `vm:power`, `vm:delete`, ...).

Não há criação de VM nesta fase — fatia vertical de leitura + power actions bem feita primeiro.

## 6. Error model

Formato único e estável:

```json
{
  "error": {
    "code": "VM_NOT_FOUND",
    "message": "Virtual machine was not found",
    "requestId": "req_01HV3M9Z2K"
  }
}
```

- Códigos estáveis em UPPER_SNAKE_CASE (`VM_NOT_FOUND`, `VM_INVALID_STATE`, `VALIDATION_ERROR`, `NOT_FOUND`, `INTERNAL_ERROR`...)
- `requestId` presente em **toda** resposta (header `X-Request-Id` + body em erros)
- Nunca expor stack traces, erros internos do libvirt ou detalhes sensíveis; detalhes vão para o log estruturado
- Mapeamento de status: 400 validação, 404 não encontrado, 409 estado inválido (ex.: `start` em VM já running), 500 interno

## 7. Estrutura do backend (monólito modular)

```
backend/
├── cmd/ultrav/           # main: bootstrap, config, wiring
├── internal/
│   ├── api/              # handlers HTTP, middlewares, error mapping
│   │   ├── middleware/   # request-id, logging, recovery, security headers
│   │   └── ...           # handlers por recurso (host, vms, docs)
│   ├── domain/           # tipos de domínio (VM, Host, Capabilities) e regras de estado
│   ├── hypervisor/       # interface HypervisorProvider
│   │   ├── mock/         # MockHypervisorProvider
│   │   └── libvirt/      # (fase 2)
│   └── config/           # env config (HYPERVISOR_PROVIDER, port, etc.)
└── openapi/              # geração/embedding do openapi.json
```

Regras de dependência: `api → domain`, `api → hypervisor (interface)`, `hypervisor/mock → domain`. `domain` não importa `api` nem pacotes de provider concretos. `libvirt` (fase 2) ficaria isolado em `hypervisor/libvirt` e só o `main` conhece implementações concretas.

## 8. Frontend

React + TypeScript + Vite. Consome exclusivamente a REST API (um cliente de API gerado/validado contra o OpenAPI).

- Design system enxuto, estética moderna (referências de UX: Linear, Vercel, consoles cloud modernos — sem copiar visuais)
- Páginas: Dashboard, Virtual Machines, VM Details
- Sidebar: Dashboard, Virtual Machines, Storage, Network, Backups (badge "Coming Soon"), Settings
- Ações de power (Start/Shutdown/Reboot/Force Stop) chamam a API; **zero** manipulação local de estado de VM
- Polling leve (ex.: 5s) para refresh de estado na fase 1; websockets ficam para depois

## 9. Observabilidade

Structured logging (JSON) em toda request:

```json
{"ts":"...","level":"info","requestId":"req_...","op":"startVirtualMachine","resource":"vm:erp01","duration_ms":42,"result":"ok"}
```

Audit log para ações administrativas (power actions) — log estruturado dedicado na fase 1; persistência em tabela própria quando houver auth. `/metrics` (Prometheus) preparado na arquitetura, implementado depois.

## 10. Segurança (fase 1 e direção futura)

Fase 1 (bind local / rede de confiança):

- Validação de inputs (IDs: allowlist de caracteres — sem path traversal)
- Nenhum shell, nenhum comando concatenado; sempre API de biblioteca (libvirt)
- Security headers, recovery middleware (500 genérico, sem stack trace)
- CORS restrito ao origin do frontend

Direção futura (arquitetura já prevista, não implementada agora):

- Usuários, API tokens, service accounts
- RBAC com scopes: `host:read`, `vm:read`, `vm:create`, `vm:power`, `vm:delete`, `storage:read`, `storage:write`, `backup:read`, `backup:run`, `backup:restore`
- Autenticação no edge do próprio backend (tokens Bearer), middleware de autorização por operação — possível porque cada endpoint é semanticamente específico

## 11. Direções futuras documentadas (NÃO implementar agora)

### Agent-friendly / Plan-Apply

Operações destrutivas seguirão o padrão **Plan/Apply**: o agente solicita uma intenção, o sistema retorna um plano com efeitos explícitos (o que será deletado, tamanho dos discos, idade do último backup) e exige aprovação antes do apply. Isso mantém agentes de IA dentro de operações específicas, auditáveis e autorizáveis — nunca dentro de shells.

### Job System

Operações longas (create, clone, backup, restore, migration) serão assíncronas:

```
POST /api/v1/vms  → 202 { "jobId": "job_xxx", "status": "queued" }
GET  /api/v1/jobs/{id} → { status, progress, result?, error? }
```

Infra prevista: tabela de jobs no SQLite, worker pool in-process no monólito, estados `queued → running → succeeded | failed`. Não implementado na fase 1.

### Backup (fase 3)

Antes de implementar: estudar a fundo **QEMU dirty bitmaps** e APIs de backup do libvirt (`virDomainBackupBegin` / incremental). Arquitetura alvo: full + incremental com bitmaps, scheduler, retention, verification, restore (inclusive file-level quando possível), storage local e remoto. **Sem backup incremental improvisado.**

### Multi-host / MCP

`HypervisorProvider` já isola o acesso por host; multi-host (fase 4) adicionará um registry de providers e um manager central. MCP (fase 5) será apenas um **adapter** sobre a API pública — o core nunca acopla a Claude/OpenAI/Gemini.

## 12. Deploy / Desenvolvimento

- `deploy/docker-compose.yml`: frontend (Vite build servido) + backend Go (modo mock), um comando para testar no navegador
- Desenvolvimento local sem Docker também suportado (backend mock em Go + Vite dev server com proxy `/api`)
