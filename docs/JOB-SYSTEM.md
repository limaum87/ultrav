# Job System (design futuro)

Operações longas — create VM, clone, backup, restore, migration — serão **assíncronas**. Não implementado na fase 1 (power actions são síncronas e rápidas).

## API prevista

```
POST /api/v1/vms            → 202 Accepted
{ "jobId": "job_01HV3MA2", "status": "queued" }

GET  /api/v1/jobs/job_01HV3MA2
{
  "id": "job_01HV3MA2",
  "type": "createVirtualMachine",
  "status": "running",          // queued | running | succeeded | failed
  "progressPercent": 40,
  "resource": "vm:erp02",
  "createdAt": "...",
  "result": null,               // preenchido em succeeded
  "error": null                 // error model padrão em failed
}
```

## Infra prevista (dentro do monólito)

- Tabela `jobs` no SQLite (persistência e histórico).
- Worker pool in-process; nada de filas externas (sem Redis/Kafka) — simplicidade primeiro.
- Jobs idempotentes quando possível; status/progresso consultáveis pela UI e por agentes.

Reavaliar necessidade de filas externas apenas quando multi-host (fase 4) existir de fato.
