# Agent-Friendly Design

A API é projetada para ser consumida com segurança por agentes de IA **no futuro** (fase 5). IA não faz parte da milestone atual; estas são decisões de design que a fundação já incorpora.

## Regras

1. **Sem endpoints genéricos perigosos.** Nunca existirão `POST /shell`, `POST /execute`, `POST /command`. Toda operação é semanticamente específica (`startVirtualMachine`, `createBackup`, `restoreBackup`), o que permite:
   - autorização individual por scope;
   - auditoria precisa;
   - compreensão semântica por LLMs (o OpenAPI é autoexplicativo).

2. **Plan/Apply para operações destrutivas.** Antes de aplicar, o sistema retorna um plano com efeitos explícitos e exige aprovação:

   ```
   Agente:  "Delete VM erp-old"
   Sistema: PLAN deleteVirtualMachine(erp-old)
            Effects:
              - VM will be removed
              - 120 GB qcow2 disk will be deleted
              - latest backup is 37 days old
            Requires approval: YES
   ```
   Somente após aprovação o apply é executado. (Não implementado nesta fase — apenas direção arquitetural.)

3. **Capabilities-first.** Agentes (e a UI) descobrem dinamicamente o que o host suporta via `getCapabilities`, sem assumir features.

4. **MCP é apenas um adapter.** O core nunca acopla a Claude, OpenAI, Gemini ou qualquer provedor. Um futuro MCP server será apenas mais um cliente da API pública, ao lado de UI, CLI e automações.
