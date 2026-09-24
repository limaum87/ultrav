import { useCallback, useMemo, useState } from 'react';
import { api, unwrap, ApiError, type Backup, type BackupSchedule, type VirtualMachine } from '../api/client';
import { formatBytes, usePolling } from '../lib/hooks';
import { ActionMenu, ConfirmDialog, EmptyState, MetricCard, TableSkeleton, type MenuItem } from '../components/ui';
import { Tabs } from '../components/Tabs';
import { CreateScheduleModal } from '../components/ScheduleModal';
import { useToast } from '../components/Toast';
import { ArchiveRestore, DatabaseBackup, Clock, HardDrive, Plus } from 'lucide-react';

type Tab = 'points' | 'schedules';

function fmtDateTime(iso: string): string {
  const d = new Date(iso);
  return d.toLocaleString('pt-BR', { dateStyle: 'short', timeStyle: 'short' });
}

export default function Backups() {
  const toast = useToast();
  const [tab, setTab] = useState<Tab>('points');
  const [busy, setBusy] = useState<string | null>(null);

  // --- backup points ---
  const vms = usePolling(async () => unwrap(api.GET('/vms')), 15000);
  const [selectedVM, setSelectedVM] = useState<string>('');
  const vmList = vms.data?.items ?? [];
  const activeVM = selectedVM || vmList[0]?.id || '';
  const points = usePolling(
    async () => {
      if (!activeVM) return { items: [] as Backup[], total: 0 };
      return unwrap(api.GET('/vms/{id}/backups', { params: { path: { id: activeVM } } }));
    },
    activeVM ? 5000 : 3600000,
  );
  const backups = points.data?.items ?? [];

  // --- schedules ---
  const schedules = usePolling(async () => unwrap(api.GET('/backup-schedules')));
  const [scheduleModal, setScheduleModal] = useState<{ open: boolean; initial: BackupSchedule | null }>({
    open: false,
    initial: null,
  });
  const [toDelete, setToDelete] = useState<{ kind: 'backup' | 'schedule'; id: string; label: string } | null>(null);
  const [toRestore, setToRestore] = useState<Backup | null>(null);

  const refreshAll = useCallback(() => {
    void points.refresh();
    void schedules.refresh();
  }, [points, schedules]);

  const run = useCallback(
    async (key: string, action: () => Promise<string>) => {
      setBusy(key);
      try {
        const msg = await action();
        toast.push('success', msg);
        refreshAll();
      } catch (e) {
        toast.push('error', e instanceof ApiError ? `${e.code} — ${e.message}` : String(e));
      } finally {
        setBusy(null);
      }
    },
    [toast, refreshAll],
  );

  const createBackup = useCallback(
    (vmId: string, type: 'auto' | 'full' | 'incremental') =>
      run(`backup-${vmId}`, async () => {
        // Assíncrono: 202 com a task; acompanhe em Tasks.
        const task = await unwrap(
          api.POST('/vms/{id}/backups', {
            params: { path: { id: vmId } },
            body: type === 'auto' ? {} : { type },
          }),
        );
        return `Backup ${type === 'auto' ? '' : `(${type}) `}de ${vmId} iniciado (task ${task?.id})`;
      }),
    [run],
  );

  const restore = useCallback(
    (b: Backup) =>
      run(`restore-${b.id}`, async () => {
        const task = await unwrap(api.POST('/backups/{id}/restore', { params: { path: { id: b.id } } }));
        return `Restore de ${b.vmId} iniciado (ponto ${b.id}, task ${task?.id})`;
      }),
    [run],
  );

  const removeBackup = useCallback(
    (b: Backup) =>
      run(`del-${b.id}`, async () => {
        await unwrap(api.DELETE('/backups/{id}', { params: { path: { id: b.id } } }));
        return `Backup ${b.id} removido`;
      }),
    [run],
  );

  const removeSchedule = useCallback(
    (sch: BackupSchedule) =>
      run(`del-${sch.id}`, async () => {
        await unwrap(api.DELETE('/backup-schedules/{id}', { params: { path: { id: sch.id } } }));
        return `Schedule “${sch.name}” removido`;
      }),
    [run],
  );

  const totalSize = useMemo(() => backups.reduce((s, b) => s + b.sizeBytes, 0), [backups]);
  const incrCount = backups.filter((b) => b.type === 'incremental').length;

  return (
    <div className="page page-wide">
      <header className="page-head">
        <div>
          <h1>Backups</h1>
          <p className="subtitle">Pontos de restauração por VM e agendamentos com retenção</p>
        </div>
        {tab === 'points' ? (
          <div className="btn-row">
            <select
              value={activeVM}
              onChange={(e) => setSelectedVM(e.target.value)}
              aria-label="Select virtual machine"
              style={{ minWidth: 180 }}
            >
              {vmList.map((vm: VirtualMachine) => (
                <option key={vm.id} value={vm.id}>
                  {vm.id}
                </option>
              ))}
            </select>
            <button
              className="btn btn-primary"
              disabled={!activeVM || busy !== null}
              onClick={() => void createBackup(activeVM, 'auto')}
            >
              <Plus size={14} aria-hidden /> {busy === `backup-${activeVM}` ? 'Backing up…' : 'Backup Now'}
            </button>
          </div>
        ) : (
          <button className="btn btn-primary" onClick={() => setScheduleModal({ open: true, initial: null })}>
            <Plus size={14} aria-hidden /> New Schedule
          </button>
        )}
      </header>

      <Tabs
        tabs={[
          { id: 'points', label: 'Backup Points', icon: <ArchiveRestore size={14} aria-hidden /> },
          { id: 'schedules', label: 'Schedules', icon: <Clock size={14} aria-hidden /> },
        ]}
        active={tab}
        onChange={(t: string) => setTab(t as Tab)}
      />

      {tab === 'points' && (
        <>
          <section className="metric-grid">
            <MetricCard
              label="Backup points"
              value={points.data?.total ?? 0}
              hint={
                <>
                  {activeVM && `VM ${activeVM}`}
                  {incrCount > 0 && ` · ${incrCount} incremental(is)`}
                </>
              }
              loading={points.loading}
              icon={<DatabaseBackup size={24} className="ic ic-blue" strokeWidth={1.75} aria-hidden />}
            />
            <MetricCard
              label="Stored"
              value={formatBytes(totalSize, 1)}
              hint="imagens no diretório de backups"
              loading={points.loading}
              icon={<HardDrive size={24} className="ic ic-purple" strokeWidth={1.75} aria-hidden />}
            />
          </section>

          <div className="card table-card">
            {points.loading ? (
              <TableSkeleton rows={3} cols={6} />
            ) : !activeVM ? (
              <EmptyState title="Nenhuma VM" message="Crie uma VM para começar a fazer backups." />
            ) : backups.length === 0 ? (
              <EmptyState
                title={`Sem backups de ${activeVM}`}
                message="Use “Backup Now” ou crie um schedule — a primeira execução sempre tira um full."
              />
            ) : (
              <table className="table">
                <thead>
                  <tr>
                    <th>Created</th>
                    <th>Type</th>
                    <th>Chain</th>
                    <th>Size</th>
                    <th>Disks</th>
                    <th>Domain XML</th>
                    <th className="th-actions">Actions</th>
                  </tr>
                </thead>
                <tbody>
                  {backups.map((b) => {
                    const menu: MenuItem[] = [
                      {
                        label: 'Restore…',
                        onSelect: () => setToRestore(b),
                        disabled: busy !== null,
                        title: 'Sobrescreve os discos da VM com este ponto (VM precisa estar parada)',
                      },
                      {
                        label: 'Delete',
                        danger: true,
                        onSelect: () => setToDelete({ kind: 'backup', id: b.id, label: b.id }),
                        disabled: busy !== null,
                        title: 'Remove permanentemente as imagens deste ponto',
                      },
                    ];
                    return (
                      <tr key={b.id} className={busy === `del-${b.id}` || busy === `restore-${b.id}` ? 'row-busy' : undefined}>
                        <td className="mono">{fmtDateTime(b.createdAt)}</td>
                        <td>
                          <span className={`state ${b.type === 'full' ? 'state-running' : ''}`}>
                            <span className="state-dot" />
                            {b.type}
                          </span>
                        </td>
                        <td className="cell-dim mono">{b.parentId ?? '—'}</td>
                        <td>{formatBytes(b.sizeBytes, 1)}</td>
                        <td className="cell-dim">{b.disks.map((d) => d.name).join(', ')}</td>
                        <td>{b.hasDomainXml ? '✓' : '—'}</td>
                        <td className="td-actions">
                          <ActionMenu items={menu} label={`Actions for ${b.id}`} />
                        </td>
                      </tr>
                    );
                  })}
                </tbody>
              </table>
            )}
          </div>
        </>
      )}

      {tab === 'schedules' && (
        <div className="card table-card">
          {schedules.loading ? (
            <TableSkeleton rows={2} cols={7} />
          ) : (schedules.data?.items ?? []).length === 0 ? (
            <EmptyState
              title="Nenhum agendamento"
              message="Crie um schedule para rodar backups diários com retenção automática."
            />
          ) : (
            <table className="table">
              <thead>
                <tr>
                  <th>Name</th>
                  <th>Time</th>
                  <th>Targets</th>
                  <th>Type</th>
                  <th>Retention</th>
                  <th>Enabled</th>
                  <th>Last run</th>
                  <th className="th-actions">Actions</th>
                </tr>
              </thead>
              <tbody>
                {(schedules.data?.items ?? []).map((sch) => {
                  const menu: MenuItem[] = [
                    {
                      label: 'Edit',
                      onSelect: () => setScheduleModal({ open: true, initial: sch }),
                      disabled: busy !== null,
                    },
                    {
                      label: sch.enabled ? 'Disable' : 'Enable',
                      onSelect: () =>
                        void run(`tgl-${sch.id}`, async () => {
                          await unwrap(api.PUT('/backup-schedules/{id}', {
                            params: { path: { id: sch.id } },
                            body: { enabled: !sch.enabled },
                          }));
                          return `Schedule “${sch.name}” ${sch.enabled ? 'desativado' : 'ativado'}`;
                        }),
                      disabled: busy !== null,
                    },
                    {
                      label: 'Delete',
                      danger: true,
                      onSelect: () => setToDelete({ kind: 'schedule', id: sch.id, label: sch.name }),
                      disabled: busy !== null,
                      title: 'Remove apenas a definição; pontos de backup são mantidos',
                    },
                  ];
                  return (
                    <tr key={sch.id} className={busy === `del-${sch.id}` ? 'row-busy' : undefined}>
                      <td className="vm-link">{sch.name}</td>
                      <td className="mono">{sch.time}</td>
                      <td className="cell-dim">{sch.vmIds.join(', ')}</td>
                      <td className="cell-dim">{sch.type}</td>
                      <td className="cell-dim">{sch.retentionKeepLast > 0 ? `keep ${sch.retentionKeepLast} chains` : '—'}</td>
                      <td>
                        <span className={`state${sch.enabled ? ' state-running' : ''}`}>
                          <span className="state-dot" />
                          {sch.enabled ? 'enabled' : 'disabled'}
                        </span>
                      </td>
                      <td className="mono">{sch.lastRun ? fmtDateTime(sch.lastRun) : '—'}</td>
                      <td className="td-actions">
                        <ActionMenu items={menu} label={`Actions for ${sch.name}`} />
                      </td>
                    </tr>
                  );
                })}
              </tbody>
            </table>
          )}
        </div>
      )}

      <CreateScheduleModal
        open={scheduleModal.open}
        initial={scheduleModal.initial}
        onClose={() => setScheduleModal({ open: false, initial: null })}
        onSaved={refreshAll}
      />

      <ConfirmDialog
        open={toRestore !== null}
        title={`Restore ${toRestore?.id ?? ''}?`}
        message={
          <>
            Os discos de <strong>{toRestore?.vmId}</strong> serão <strong>sobrescritos</strong> com o conteúdo
            deste ponto (full {toRestore?.parentId ? '+ incrementais da cadeia' : ''}). A VM precisa estar
            <strong> parada</strong>. O conteúdo atual dos discos será perdido.
          </>
        }
        confirmLabel="Restore Now"
        busy={busy !== null}
        onCancel={() => setToRestore(null)}
        onConfirm={() => {
          const b = toRestore;
          setToRestore(null);
          if (b) void restore(b);
        }}
      />

      <ConfirmDialog
        open={toDelete !== null}
        title={toDelete?.kind === 'backup' ? `Delete backup ${toDelete?.id}?` : `Delete schedule “${toDelete?.label}”?`}
        message={
          toDelete?.kind === 'backup'
            ? 'As imagens deste ponto são removidas permanentemente do diretório de backups. Se ele for o pai de incrementais, a cadeia deles fica quebrada.'
            : 'Apenas a definição do agendamento é removida; pontos de backup existentes são mantidos.'
        }
        confirmLabel="Delete"
        busy={busy !== null}
        onCancel={() => setToDelete(null)}
        onConfirm={() => {
          const t = toDelete;
          setToDelete(null);
          if (!t) return;
          if (t.kind === 'backup') {
            const found = backups.find((b) => b.id === t.id);
            if (found) void removeBackup(found);
          } else {
            const sch = (schedules.data?.items ?? []).find((x) => x.id === t.id);
            if (sch) void removeSchedule(sch);
          }
        }}
      />
    </div>
  );
}
