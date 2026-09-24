import { useEffect, useMemo, useState } from 'react';
import { api, unwrap, ApiError, type StoragePool, type Network, type Iso } from '../api/client';
import { useToast } from './Toast';
import { formatBytes } from '../lib/hooks';
import { Monitor, Check, X, Rocket } from 'lucide-react';

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
  isoId: string | null;
  osType: 'linux' | 'windows' | 'other';
  /** '' = follow the osType profile (virtio, or e1000e for other). */
  nicModel: '' | 'virtio' | 'e1000e' | 'rtl8139';
  virtioDriversIsoId: string | null;
  start: boolean;
};

const STEPS = ['General', 'Storage', 'Network', 'Media', 'Review'] as const;

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
  const [isos, setIsos] = useState<Iso[]>([]);
  const [form, setForm] = useState<Form>({
    name: '',
    vcpus: 2,
    memoryGiB: 4,
    poolId: '',
    diskGiB: 40,
    format: 'qcow2',
    networkId: 'default',
    isoId: null,
    osType: 'linux',
    nicModel: '',
    virtioDriversIsoId: null,
    start: false,
  });
  const toast = useToast();

  const set = <K extends keyof Form>(k: K, v: Form[K]) =>
    setForm((f) => ({ ...f, [k]: v }));

  // What the backend picks when nicModel is left empty (osType profile).
  const autoNic = form.osType === 'other' ? 'e1000e' : 'virtio';
  const effectiveNic = form.nicModel || autoNic;

  // Load pools + networks when the wizard opens; default selections.
  useEffect(() => {
    if (!open) return;
    setStep(0);
    setError(null);
    (async () => {
      try {
        const [p, n, i] = await Promise.all([
          unwrap(api.GET('/storage/pools')),
          unwrap(api.GET('/networks')),
          unwrap(api.GET('/storage/isos')),
        ]);
        setPools(p.items);
        setNetworks(n.items);
        setIsos(i.items);
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
    const errs: string[] = ['', '', '', '', ''];
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
      const task = await unwrap(
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
            isoId: form.isoId,
            osType: form.osType,
            ...(form.nicModel ? { nicModel: form.nicModel } : {}),
            ...(form.osType === 'windows' ? { virtioDriversIsoId: form.virtioDriversIsoId } : {}),
            start: form.start,
          },
        }),
      );
      onCreated();
      toast.push(
        'success',
        `Creation of ${form.name} started (task ${task?.id}). Follow the progress in Tasks.`,
      );
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
          <div className="wiz-head-title">
            <span className="wiz-head-icon ic-bg-violet">
              <Monitor size={18} className="ic ic-violet" aria-hidden />
            </span>
            <h2>Create Virtual Machine</h2>
          </div>
          <button className="wiz-close" onClick={onClose} aria-label="Close" disabled={submitting}>
            <X size={16} aria-hidden />
          </button>
        </header>

        <ol className="wiz-steps">
          {STEPS.map((label, i) => (
            <li
              key={label}
              className={i === step ? 'current' : i < step ? 'done' : ''}
              onClick={() => i < step && setStep(i)}
            >
              <span className="wiz-step-num">{i < step ? <Check size={11} strokeWidth={3} aria-hidden /> : i + 1}</span>
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
              <label className="field">
                <span>Operating system</span>
                <select
                  value={form.osType}
                  onChange={(e) => {
                    const v = e.target.value as Form['osType'];
                    setForm((f) => ({ ...f, osType: v, virtioDriversIsoId: v === 'windows' ? f.virtioDriversIsoId : null }));
                  }}
                  disabled={submitting}
                >
                  <option value="linux">Linux</option>
                  <option value="windows">Windows</option>
                  <option value="other">Other (max compatibility)</option>
                </select>
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
              <label className="field">
                <span>Adapter model</span>
                <select
                  value={form.nicModel}
                  onChange={(e) => set('nicModel', e.target.value as Form['nicModel'])}
                  disabled={submitting}
                >
                  <option value="">Automatic — {autoNic} (from the {form.osType} profile)</option>
                  <option value="virtio">virtio — fastest, needs a driver in the guest</option>
                  <option value="e1000e">e1000e — emulated Intel, works out of the box</option>
                  <option value="rtl8139">rtl8139 — emulated Realtek, for very old guests</option>
                </select>
              </label>
              <p className="wiz-hint">
                The VM gets one {effectiveNic} NIC on the selected network.
              </p>
              {form.osType === 'windows' && effectiveNic === 'virtio' && (
                <p className="alert warn">
                  Windows has no in-box virtio driver: this VM boots without network until you
                  install NetKVM from the VirtIO drivers ISO (Device Manager shows the adapter
                  with error 43 or as an unknown Ethernet controller). Choose <strong>e1000e</strong>{' '}
                  if it needs network on first boot.
                </p>
              )}
            </div>
          )}

          {step === 3 && (
            <div className="wiz-form">
              <label className="field">
                <span>Installation media (ISO)</span>
                <select
                  value={form.isoId ?? ''}
                  onChange={(e) => set('isoId', e.target.value || null)}
                  disabled={submitting}
                >
                  <option value="">None — attach later (virt-manager / virt-install)</option>
                  {isos.map((iso) => (
                    <option key={iso.id} value={iso.id}>
                      {iso.fileName} ({formatBytes(iso.sizeBytes, 0)})
                    </option>
                  ))}
                </select>
              </label>
              {isos.length === 0 && (
                <p className="wiz-hint">
                  No ISOs in the library yet — upload one in Storage → ISO Library.
                </p>
              )}
              {form.isoId && (
                <p className="wiz-hint">
                  The ISO is attached as a SATA CD-ROM and set as the first boot device,
                  so the VM boots into the installer. You can eject it after install.
                </p>
              )}
              {form.osType === 'windows' && (
                <>
                  <label className="field">
                    <span>VirtIO drivers ISO (Windows)</span>
                    <select
                      value={form.virtioDriversIsoId ?? ''}
                      onChange={(e) => set('virtioDriversIsoId', e.target.value || null)}
                      disabled={submitting}
                    >
                      <option value="">None — installer will not see the virtio disk</option>
                      {isos
                        .filter((iso) => iso.fileName.toLowerCase().includes('virtio'))
                        .map((iso) => (
                          <option key={iso.id} value={iso.id}>
                            {iso.fileName} ({formatBytes(iso.sizeBytes, 0)})
                          </option>
                        ))}
                    </select>
                  </label>
                  <p className="wiz-hint">
                    Without the drivers ISO the Windows installer cannot see the virtio-scsi
                    disk. Download the stable virtio-win ISO from{' '}
                    <a href="https://fedorapeople.org/groups/virt/virtio-win/direct-downloads/stable-virtio/" target="_blank" rel="noreferrer">
                      fedorapeople (virtio-win stable)
                    </a>{' '}
                    and upload it in Storage → ISO Library. It is attached as a second CD-ROM.
                  </p>
                </>
              )}
            </div>
          )}

          {step === 4 && (
            <div className="wiz-review">
              <div className="kv"><span className="kv-key">Name</span><span className="kv-val">{form.name}</span></div>
              <div className="kv"><span className="kv-key">vCPUs</span><span className="kv-val">{form.vcpus}</span></div>
              <div className="kv"><span className="kv-key">Memory</span><span className="kv-val">{form.memoryGiB} GiB</span></div>
              <div className="kv"><span className="kv-key">Disk</span><span className="kv-val">{form.diskGiB} GiB {form.format} · pool “{form.poolId}”</span></div>
              <div className="kv"><span className="kv-key">Network</span><span className="kv-val">{form.networkId} ({effectiveNic})</span></div>
              <div className="kv"><span className="kv-key">Install media</span><span className="kv-val">{form.isoId ?? 'none'}</span></div>
              <label className="field field-check" style={{ marginTop: 12 }}>
                <input
                  type="checkbox"
                  checked={form.start}
                  onChange={(e) => set('start', e.target.checked)}
                  disabled={submitting}
                />
                <span>Power on immediately after creation</span>
              </label>
              <div className="kv"><span className="kv-key">Power on</span><span className="kv-val">{form.start ? 'yes' : 'no (left stopped)'}</span></div>
            </div>
          )}

          {stepErrors[step] && step !== STEPS.length - 1 && <div className="wiz-error">{stepErrors[step]}</div>}
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
                className="btn btn-primary btn-with-icon"
                onClick={() => void submit()}
                disabled={submitting || poolFull}
              >
                <Rocket size={14} strokeWidth={2} aria-hidden />
                {submitting ? 'Creating…' : 'Create VM'}
              </button>
            ) : (
              <button
                className="btn btn-primary"
                onClick={() => setStep((s) => s + 1)}
                disabled={!canNext || submitting}
              >
                Next
              </button>
            )}
          </div>
        </footer>
      </div>
    </div>
  );
}
