import { useEffect, useMemo, useState } from 'react';
import { api, unwrap, ApiError, type StoragePool, type Network } from '../api/client';
import { formatBytes } from '../lib/hooks';

const NAME_RE = /^[a-zA-Z0-9][a-zA-Z0-9._-]{0,63}$/;
const GiB = 1024 * 1024 * 1024;

type Form = {
  name: string;
  vcpus: number;
  memoryGiB: number;
  poolId: string;
  diskGiB: number;
  format: 'qcow2' | 'raw';
  networkId: string;
  start: boolean;
};

const STEPS = ['General', 'Storage', 'Network', 'Review'] as const;

export function CreateVMWizard({
  open,
  onClose,
  onCreated,
}: {
  open: boolean;
  onClose: () => void;
  onCreated: () => void;
}) {
  const [step, setStep] = useState(0);
  const [submitting, setSubmitting] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [pools, setPools] = useState<StoragePool[]>([]);
  const [networks, setNetworks] = useState<Network[]>([]);
  const [form, setForm] = useState<Form>({
    name: '',
    vcpus: 2,
    memoryGiB: 4,
    poolId: '',
    diskGiB: 40,
    format: 'qcow2',
    networkId: 'default',
    start: false,
  });

  const set = <K extends keyof Form>(k: K, v: Form[K]) =>
    setForm((f) => ({ ...f, [k]: v }));

  // Load pools + networks when the wizard opens; default selections.
  useEffect(() => {
    if (!open) return;
    setStep(0);
    setError(null);
    (async () => {
      try {
        const [p, n] = await Promise.all([
          unwrap(api.GET('/storage/pools')),
          unwrap(api.GET('/networks')),
        ]);
        setPools(p.items);
        setNetworks(n.items);
        setForm((f) => ({
          ...f,
          poolId: f.poolId || p.items.find((x) => x.state === 'active')?.id || p.items[0]?.id || '',
          networkId: f.networkId || n.items.find((x) => x.state === 'active')?.id || n.items[0]?.id || '',
        }));
      } catch (e) {
        setError(e instanceof ApiError ? `${e.code}: ${e.message}` : String(e));
      }
    })();
  }, [open]);

  const stepErrors = useMemo<string[]>(() => {
    const errs: string[] = ['', '', '', ''];
    if (!NAME_RE.test(form.name)) errs[0] = 'Name must start with a letter/digit (a-z0-9._-, max 64 chars)';
    else if (form.vcpus < 1 || form.vcpus > 64) errs[0] = 'vCPUs must be between 1 and 64';
    else if (form.memoryGiB < 0.016) errs[0] = 'Memory must be at least 16 MiB';
    if (!form.poolId) errs[1] = 'Select a storage pool';
    else if (form.diskGiB < 1) errs[1] = 'Disk size must be at least 1 GiB';
    if (!form.networkId) errs[2] = 'Select a network';
    return errs;
  }, [form]);

  if (!open) return null;

  const canNext = stepErrors[step] === '';
  const last = step === STEPS.length - 1;

  const submit = async () => {
    setSubmitting(true);
    setError(null);
    try {
      await unwrap(
        api.POST('/vms', {
          body: {
            name: form.name,
            vcpus: form.vcpus,
            memoryBytes: Math.round(form.memoryGiB * GiB),
            disk: {
              poolId: form.poolId,
              sizeBytes: Math.round(form.diskGiB * GiB),
              format: form.format,
            },
            networkId: form.networkId,
            start: form.start,
          },
        }),
      );
      onCreated();
      onClose();
    } catch (e) {
      setError(e instanceof ApiError ? `${e.code}: ${e.message}` : String(e));
    } finally {
      setSubmitting(false);
    }
  };

  const selectedPool = pools.find((p) => p.id === form.poolId);
  const poolFull =
    selectedPool != null &&
    selectedPool.availableBytes != null &&
    form.diskGiB * GiB > selectedPool.availableBytes;

  return (
    <div className="wiz-overlay" role="dialog" aria-modal="true" aria-label="Create virtual machine">
      <div className="wiz">
        <header className="wiz-head">
          <h2>Create Virtual Machine</h2>
          <button className="wiz-close" onClick={onClose} aria-label="Close" disabled={submitting}>
            ×
          </button>
        </header>

        <ol className="wiz-steps">
          {STEPS.map((label, i) => (
            <li
              key={label}
              className={i === step ? 'current' : i < step ? 'done' : ''}
              onClick={() => i < step && setStep(i)}
            >
              <span className="wiz-step-num">{i < step ? '✓' : i + 1}</span>
              {label}
            </li>
          ))}
        </ol>

        {error && <div className="alert error">{error}</div>}

        <div className="wiz-body">
          {step === 0 && (
            <div className="wiz-form">
              <label className="field">
                <span>Name</span>
                <input
                  autoFocus
                  value={form.name}
                  onChange={(e) => set('name', e.target.value)}
                  placeholder="app01"
                  disabled={submitting}
                />
              </label>
              <div className="wiz-row">
                <label className="field">
                  <span>vCPUs</span>
                  <input
                    type="number"
                    min={1}
                    max={64}
                    value={form.vcpus}
                    onChange={(e) => set('vcpus', Number(e.target.value) || 0)}
                    disabled={submitting}
                  />
                </label>
                <label className="field">
                  <span>Memory (GiB)</span>
                  <input
                    type="number"
                    min={1}
                    step={1}
                    value={form.memoryGiB}
                    onChange={(e) => set('memoryGiB', Number(e.target.value) || 0)}
                    disabled={submitting}
                  />
                </label>
              </div>
            </div>
          )}

          {step === 1 && (
            <div className="wiz-form">
              <label className="field">
                <span>Storage pool</span>
                <select
                  value={form.poolId}
                  onChange={(e) => set('poolId', e.target.value)}
                  disabled={submitting}
                >
                  {pools.map((p) => (
                    <option key={p.id} value={p.id}>
                      {p.name} ({p.state}, {formatBytes(p.availableBytes ?? 0)} free)
                    </option>
                  ))}
                  {pools.length === 0 && <option value="">No pools available</option>}
                </select>
              </label>
              <div className="wiz-row">
                <label className="field">
                  <span>Disk size (GiB)</span>
                  <input
                    type="number"
                    min={1}
                    value={form.diskGiB}
                    onChange={(e) => set('diskGiB', Number(e.target.value) || 0)}
                    disabled={submitting}
                  />
                </label>
                <label className="field">
                  <span>Format</span>
                  <select
                    value={form.format}
                    onChange={(e) => set('format', e.target.value as 'qcow2' | 'raw')}
                    disabled={submitting}
                  >
                    <option value="qcow2">qcow2</option>
                    <option value="raw">raw</option>
                  </select>
                </label>
              </div>
              {poolFull && (
                <div className="alert warn">
                  Disk size exceeds the free space in “{selectedPool?.name}” ({formatBytes(selectedPool?.availableBytes ?? 0)}).
                </div>
              )}
            </div>
          )}

          {step === 2 && (
            <div className="wiz-form">
              <label className="field">
                <span>Network</span>
                <select
                  value={form.networkId}
                  onChange={(e) => set('networkId', e.target.value)}
                  disabled={submitting}
                >
                  {networks.map((n) => (
                    <option key={n.id} value={n.id}>
                      {n.name} ({n.state}{n.ipAddress ? `, ${n.ipAddress}/${n.ipPrefix}` : ''})
                    </option>
                  ))}
                  {networks.length === 0 && <option value="">No networks available</option>}
                </select>
              </label>
              <label className="field field-check">
                <input
                  type="checkbox"
                  checked={form.start}
                  onChange={(e) => set('start', e.target.checked)}
                  disabled={submitting}
                />
                <span>Power on immediately after creation</span>
              </label>
              <p className="wiz-hint">
                The VM is defined with a virtio disk and one virtio NIC on the selected network.
                Attach install media (ISO) via virt-manager/virt-install to install an OS.
              </p>
            </div>
          )}

          {step === 3 && (
            <div className="wiz-review">
              <div className="kv"><span className="kv-key">Name</span><span className="kv-val">{form.name}</span></div>
              <div className="kv"><span className="kv-key">vCPUs</span><span className="kv-val">{form.vcpus}</span></div>
              <div className="kv"><span className="kv-key">Memory</span><span className="kv-val">{form.memoryGiB} GiB</span></div>
              <div className="kv"><span className="kv-key">Disk</span><span className="kv-val">{form.diskGiB} GiB {form.format} · pool “{form.poolId}”</span></div>
              <div className="kv"><span className="kv-key">Network</span><span className="kv-val">{form.networkId} (virtio)</span></div>
              <div className="kv"><span className="kv-key">Power on</span><span className="kv-val">{form.start ? 'yes' : 'no (left stopped)'}</span></div>
            </div>
          )}

          {stepErrors[step] && step !== 3 && <div className="wiz-error">{stepErrors[step]}</div>}
        </div>

        <footer className="wiz-foot">
          <button className="btn" onClick={onClose} disabled={submitting}>
            Cancel
          </button>
          <div className="wiz-foot-right">
            {step > 0 && (
              <button className="btn" onClick={() => setStep((s) => s - 1)} disabled={submitting}>
                ← Back
              </button>
            )}
            {last ? (
              <button
                className="btn btn-primary"
                onClick={() => void submit()}
                disabled={submitting || poolFull}
              >
                {submitting ? 'Creating…' : 'Create VM'}
              </button>
            ) : (
              <button
                className="btn btn-primary"
                onClick={() => setStep((s) => s + 1)}
                disabled={!canNext || submitting}
              >
                Next →
              </button>
            )}
          </div>
        </footer>
      </div>
    </div>
  );
}
