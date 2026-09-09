# Backup (design futuro — NÃO implementar na fase 1)

Backup será um dos principais diferenciais do UltraV (fase 3). **Não haverá backup incremental improvisado.** Antes de implementar, estudo obrigatório de:

- **QEMU dirty bitmaps** (`persistent` + `inactive` bitmaps, `qemu-monitor-command`/QMP)
- **APIs de backup do libvirt**: `virDomainBackupBegin` (push mode), incremental via `virDomainBackupBegin` com checkpoints

## Arquitetura alvo

- Full backup e incremental (dirty bitmaps + checkpoints do libvirt)
- Scheduler (cron interno do monólito)
- Retention policies (GFS simples: daily/weekly/monthly)
- Verification (checksum + teste de abertura da imagem pós-backup)
- Restore: VM inteira; file-level quando possível (via guest agent / mount de imagem)
- Storage: local primeiro; remoto (S3-compatible) depois
- Integração com o Job System (`backup:run`, `backup:restore` scopes)

## Por que não agora

Dirty bitmaps e checkpoints têm pegadinhas reais (bitmaps não persistentes são perdidos em shutdown, consistência entre discos, crash durante backup incremental). A fase 3 só começa com um spike documentado em `docs/` validando a abordagem contra QEMU 8.x + libvirt 10.x.
