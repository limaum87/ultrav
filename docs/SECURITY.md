# Segurança — Princípios e Direção

## Fase 1 (implementado desde o início)

- **Validação de inputs**: IDs de VM seguem allowlist `^[a-zA-Z0-9][a-zA-Z0-9._-]{0,63}$`; nenhum valor de input alcança camadas inferiores sem validação.
- **Sem shell**: nenhum comando de shell é executado; toda interação com o hipervisor é via API de biblioteca (libvirt hoje, mock na fase 1).
- **Sem path traversal**: nomes/arquivos informados pelo cliente nunca são usados para montar caminhos sem sanitização + allowlist.
- **Sem vazamento de erros**: stack traces e erros internos (incl. mensagens do libvirt) ficam no log; cliente recebe apenas o error model estável.
- **Correlation ID**: `X-Request-Id` gerado ou propagado em toda request, incluído no body de erros e no log estruturado.
- **Security headers** e recovery middleware (500 genérico).
- **Audit log** para ações administrativas (power actions) em log estruturado dedicado.
- **CORS** restrito ao origin do frontend.

## Direção futura (arquitetura prevista, não implementada)

- Autenticação: usuários, **API tokens** e **service accounts** (Bearer).
- **RBAC por scopes**, aplicado por operação (viabilizado pelo design de operações específicas):

  | Scope            | Operações                                    |
  |------------------|----------------------------------------------|
  | `host:read`      | `getHost`, `getCapabilities`                 |
  | `vm:read`        | `listVirtualMachines`, `getVirtualMachine`   |
  | `vm:power`       | `start/shutdown/reboot/forceStop`            |
  | `vm:create`      | `createVirtualMachine` (fase 2)              |
  | `vm:delete`      | `deleteVirtualMachine` (futuro, Plan/Apply)  |
  | `storage:read` / `storage:write` | storage pools               |
  | `backup:read` / `backup:run` / `backup:restore`  | backup      |

- Operações destrutivas exigirão **Plan/Apply** (ver `AGENT-FRIENDLY.md`).
