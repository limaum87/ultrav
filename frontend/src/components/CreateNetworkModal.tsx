import { useEffect, useState } from 'react';
import { api, unwrap, ApiError, type HostBridgeList } from '../api/client';

const NAME_RE = /^[a-zA-Z0-9][a-zA-Z0-9._-]{0,63}$/;
const IFACE_RE = /^[a-zA-Z0-9][a-zA-Z0-9._-]{0,15}$/;
const CIDR_RE = /^([0-9]{1,3}\.){3}[0-9]{1,3}\/([0-9]|[12][0-9]|3[0-2])$/;

type Mode = 'nat' | 'bridge' | 'isolated';

const MODE_HINTS: Record<Mode, string> = {
  nat: 'Private subnet with DHCP and NAT; VMs reach the internet but are not reachable from the LAN.',
  bridge: 'Attach to a host bridge (e.g. br0): VMs get an IP on the host\u2019s LAN from your router \u2014 same range as the host.',
  isolated: 'Internal-only network; VMs can talk to each other but have no outside connectivity.',
};

export function CreateNetworkModal({
  open,
  onClose,
  onCreated,
}: {
  open: boolean;
  onClose: () => void;
  onCreated: () => void;
}) {
  const [name, setName] = useState('');
  const [mode, setMode] = useState<Mode>('nat');
  const [cidr, setCidr] = useState('192.168.100.0/24');
  const [bridgeName, setBridgeName] = useState('');
  const [dhcp, setDhcp] = useState(true);
  const [autostart, setAutostart] = useState(true);
  const [bridges, setBridges] = useState<HostBridgeList | null>(null);
  const [submitting, setSubmitting] = useState(false);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    if (open && mode === 'bridge' && bridges === null) {
      unwrap(api.GET('/host/bridges')).then(setBridges).catch(() => setBridges({ items: [], total: 0 }));
    }
  }, [open, mode, bridges]);

  if (!open) return null;

  const nameOk = NAME_RE.test(name);
  const cidrOk = mode === 'bridge' || CIDR_RE.test(cidr);
  const bridgeOk = mode !== 'bridge' || (IFACE_RE.test(bridgeName) && bridgeName.length > 0);
  const valid = nameOk && cidrOk && bridgeOk;

  const submit = async () => {
    setSubmitting(true);
    setError(null);
    try {
      await unwrap(
        api.POST('/networks', {
          body:
            mode === 'bridge'
              ? { name, mode, bridgeName, autostart }
              : { name, mode, cidr, dhcpEnabled: dhcp, autostart },
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

  return (
    <div className="wiz-overlay" role="dialog" aria-modal="true" aria-label="Create virtual network">
      <div className="wiz">
        <header className="wiz-head">
          <h2>Create Virtual Network</h2>
          <button className="wiz-close" onClick={onClose} aria-label="Close" disabled={submitting}>
            ×
          </button>
        </header>

        {error && <div className="alert error">{error}</div>}

        <div className="wiz-form" style={{ padding: 20 }}>
          <label className="field">
            <span>Name</span>
            <input
              autoFocus
              value={name}
              onChange={(e) => setName(e.target.value)}
              placeholder={mode === 'bridge' ? 'lan' : 'lab-net'}
              disabled={submitting}
            />
            {name && !nameOk && (
              <small className="field-error">
                Name must start with a letter/digit (a-z0-9._-, max 64 chars)
              </small>
            )}
          </label>

          <div className="field">
            <span>Mode</span>
            <div className="mode-toggle" role="radiogroup">
              {(['nat', 'bridge', 'isolated'] as Mode[]).map((m) => (
                <button
                  key={m}
                  type="button"
                  role="radio"
                  aria-checked={mode === m}
                  className={`btn mode-btn ${mode === m ? 'btn-primary' : ''}`}
                  onClick={() => setMode(m)}
                  disabled={submitting}
                >
                  {m === 'bridge' ? 'bridge (LAN)' : m}
                </button>
              ))}
            </div>
            <small className="field-hint">{MODE_HINTS[mode]}</small>
          </div>

          {mode === 'bridge' ? (
            <>
              {bridges && bridges.total === 0 && (
                <div className="alert warn">
                  <strong>No host bridges found.</strong> A bridge (e.g. <code>br0</code>) must exist
                  on the host before VMs can get LAN IPs — creating one is host configuration, not
                  something UltraV does. On Ubuntu/Debian with netplan:
                  <pre>{`# /etc/netplan/60-br0.yaml
network:
  version: 2
  bridges:
    br0:
      interfaces: [enp3s0]   # your physical NIC
      addresses: [192.168.1.10/24]
      routes:
        - to: default
          via: 192.168.1.1
      nameservers:
        addresses: [192.168.1.1]
# then: sudo netplan apply`}</pre>
                  After that, this page will detect it automatically. You can still type a bridge
                  name below if it exists but wasn't detected.
                </div>
              )}
              <label className="field">
                <span>Host bridge</span>
                {bridges && bridges.total > 0 ? (
                  <select
                    value={bridgeName}
                    onChange={(e) => setBridgeName(e.target.value)}
                    disabled={submitting}
                    className="mono"
                  >
                    <option value="">— select a bridge —</option>
                    {bridges.items.map((b) => (
                      <option key={b.name} value={b.name}>
                        {b.name} {b.active ? '' : '(inactive)'}
                      </option>
                    ))}
                  </select>
                ) : (
                  <input
                    value={bridgeName}
                    onChange={(e) => setBridgeName(e.target.value)}
                    placeholder="br0"
                    disabled={submitting}
                    className="mono"
                  />
                )}
                {bridgeName && !bridgeOk && (
                  <small className="field-error">Invalid interface name (max 16 chars)</small>
                )}
              </label>
            </>
          ) : (
            <>
              <label className="field">
                <span>Subnet (CIDR)</span>
                <input
                  value={cidr}
                  onChange={(e) => setCidr(e.target.value)}
                  placeholder="192.168.100.0/24"
                  disabled={submitting}
                  className="mono"
                />
                {cidr && !cidrOk && (
                  <small className="field-error">Use dotted-quad/prefix notation, e.g. 192.168.100.0/24</small>
                )}
                <small className="field-hint">
                  The gateway (first usable IP, e.g. 192.168.100.1) is assigned automatically.
                </small>
              </label>
              <label className="field field-check">
                <input
                  type="checkbox"
                  checked={dhcp}
                  onChange={(e) => setDhcp(e.target.checked)}
                  disabled={submitting}
                />
                <span>Enable DHCP (libvirt dnsmasq hands out leases in this subnet)</span>
              </label>
            </>
          )}

          <label className="field field-check">
            <input
              type="checkbox"
              checked={autostart}
              onChange={(e) => setAutostart(e.target.checked)}
              disabled={submitting}
            />
            <span>Start network automatically on boot (autostart)</span>
          </label>
        </div>

        <footer className="wiz-foot">
          <button className="btn" onClick={onClose} disabled={submitting}>
            Cancel
          </button>
          <button className="btn btn-primary" onClick={() => void submit()} disabled={!valid || submitting}>
            {submitting ? 'Creating…' : 'Create Network'}
          </button>
        </footer>
      </div>
    </div>
  );
}
