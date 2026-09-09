# UltraV — Roadmap

> Princípio: fatias verticais pequenas e bem construídas. Nenhuma fase começa sem a anterior estável.

## Phase 1 — Foundation (atual)

**Entregável:** aplicação executável via Docker Compose mostrando o host mock `kvm01` e suas VMs, com power actions funcionais pela UI e pelo Swagger.

- [x] Análise de requisitos e arquitetura
- [x] ROADMAP.md / ARCHITECTURE.md
- [x] Contrato OpenAPI 3.1 inicial (`docs/api/openapi.yaml`)
- [x] Estrutura do monorepo (backend/, frontend/, docs/, deploy/, scripts/)
- [x] Backend Go mínimo (bootstrap, config, middlewares, error model, request-id)
- [x] `MockHypervisorProvider` (host kvm01 + erp01, web01, database01, monitoring01)
- [x] Endpoints: `getHealth`, `getReadiness`, `getHost`, `getCapabilities`, `listVirtualMachines`, `getVirtualMachine`, `startVirtualMachine`, `shutdownVirtualMachine`, `rebootVirtualMachine`, `forceStopVirtualMachine`
- [x] `GET /openapi.json` + Swagger UI em `GET /docs`
- [x] Codegen: tipos Go (oapi-codegen) + client TypeScript (openapi-typescript/openapi-fetch) a partir do contrato
- [x] Frontend: Dashboard (host, storage, VMs), Virtual Machines (lista + ações), VM Details (overview, hardware, ações)
- [x] Docker Compose (backend mock + frontend, porta única com proxy)
- [x] Testes essenciais (unitários no mock/domain, integração nos handlers via httptest)
- [x] Documentação de execução local

**Fora de escopo:** libvirt real, criação de VM, backup, multi-host, auth, IA.

## Phase 2 — Real libvirt integration (em andamento)

- [x] `LibvirtHypervisorProvider` (leitura de hosts/VMs + power actions, QEMU Guest Agent p/ IPs)
- [x] `getCapabilities` real (capabilities XML do libvirt)
- [x] Storage pools: leitura + refresh (`listStoragePools`, `getStoragePool`, `refreshStoragePool`)
- [x] Networks: leitura + start/stop (`listNetworks`, `getNetwork`, `startNetwork`, `stopNetwork`)
- [ ] Instalação/target: Ubuntu Server, Debian
- [ ] `createVirtualMachine` (assíncrono → Job System v1)
- [ ] Snapshots
- [ ] Cloud-init
- [ ] Templates e clone
- [x] Console web (noVNC/WebSocket) — `GET /vms/{id}/console` (WS → VNC via OpenGraphicsFD), noVNC na UI, mock fala RFB
- [ ] Validação em host KVM real

## Phase 3 — Backup engine

Pré-requisito: estudo aprofundado de **QEMU dirty bitmaps** e APIs de backup do libvirt (`virDomainBackupBegin`, incremental). Sem improvisos.

- [ ] Full backup
- [ ] Incremental backup (dirty bitmaps)
- [ ] Scheduler
- [ ] Retention policies
- [ ] Verification de backups
- [ ] Restore (incl. file-level quando possível)
- [ ] Storage local + remoto (S3-compatible candidato)

## Phase 4 — Multi-host

- [ ] Registry de hosts / providers
- [ ] Central manager (ainda monólito)
- [ ] Migration
- [ ] Consolidação da UI para múltiplos hosts

## Phase 5 — Automation & AI

- [ ] CLI oficial (consomendo a mesma API)
- [ ] Automação / integrações (Ansible, webhooks)
- [ ] MCP adapter (a API pública já é a interface; MCP é apenas adapter)
- [ ] AI Operations Assistant com padrão **Plan/Apply** para operações destrutivas (planos com efeitos explícitos + aprovação obrigatória)
- [ ] RBAC completo com scopes (`vm:power`, `backup:run`, ...)
- [ ] `/metrics` Prometheus
