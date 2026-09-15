import { useEffect, useState } from 'react';
import { api, unwrap, ApiError, type HostBridgeList, type Network } from '../api/client';
import { Network as NetworkIcon, X, Pencil } from 'lucide-react';

const IFACE_RE = /^[a-zA-Z0-9][a-zA-Z0-9._-]{0,15}$/;
const CIDR_RE = /^([0-9]{1,3}\.){3}[0-9]{1,3}\/([0-9]|[12][0-9]|3[0-2])$/;

type Mode = 'nat' | 'bridge' | 'isolated';

export function EditNetworkModal({
  network,
  onClose,
  onUpdated,
}: {
  network: Network | null;
  onClose: () => void;
  onUpdated: () => void;
}) {
  const open = network !== null;
  const mode_ = (network?.mode ?? 'nat') as Mode;
  const active = network?.state === 'active';

  const [mode, setMode] = useState<Mode>(mode_);
  const [cidr, setCidr] = useState('');
  const [bridgeName, setBridgeName] = useState('');
  const [dhcp, setDhcp] = useState(true);
  const [autostart, setAutostart] = useState(true);
  const [bridges, setBridges] = useState<HostBridgeList | null>(null);
  const [submitting, setSubmitting] = useState(false);
  const [error, setError] = useState<string | null>(null);

  // Reset local state whenever a different network is opened.
  useEffect(() => {
    if (!network) return;
    setMode((network.mode ?? 'nat') as Mode);
    setCidr(
      network.ipAddress && network.ipPrefix
        ? `${network.ipAddress}/${network.ipPrefix}`
        : '192.168.100.0/24',
    );
    setBridgeName(network.bridge ?? '');
    setDhcp(network.dhcpEnabled);
    setAutostart(network.autostart);
    setError(null);
  }, [network]);

  useEffect(() => {
    if (open && mode === 'bridge' && bridges === null) {
      unwrap(api.GET('/host/bridges')).then(setBridges).catch(() => setBridges({ items: [], total: 0 }));
    }
  }, [open, mode, bridges]);

  if (!open || !network) return null;

  const structuralAllowed = !active;
  const cidrOk = mode === 'bridge' || CIDR_RE.test(cidr);
  const bridgeOk = mode !== 'bridge' || (IFACE_RE.test(bridgeName) && bridgeName.length > 0);
  const valid = cidrOk && bridgeOk;

  const submit = async () => {
    setSubmitting(true);
    setError(null);
    try {
      const body: Record<string, unknown> = { mode, autostart };
      if (mode === 'bridge') body.bridgeName = bridgeName;
      else {
        body.cidr = cidr;
        body.dhcpEnabled = dhcp;
      }
      await unwrap(
        api.PUT('/networks/{id}', { params: { path: { id: network.id } }, body: body as never }),
      );
      onUpdated();
      onClose();
    } catch (e) {
      setError(e instanceof ApiError ? `${e.code}: ${e.message}` : String(e));
    } finally {
      setSubmitting(false);
    }
  };

  return (
    <div className="wiz-overlay" role="dialog" aria-modal="true" aria-label={`Edit network ${network.name}`}>
      <div className="wiz">
        <header className="wiz-head">
          <div className="wiz-head-title">
            <span className="wiz-head-icon ic-bg-green">
              <NetworkIcon size={18} className="ic ic-green" aria-hidden />
            </span>
            <h2>Edit Network — {network.name}</h2>
          </div>
          <button className="wiz-close" onClick={onClose} aria-label="Close" disabled={submitting}>
            <X size={16} aria-hidden />
          </button>
        </header>

        {error && <div className="alert error">{error}</div>}
        {active && (
          <div className="alert warn">
            This network is <strong>active</strong>. Only <em>autostart</em> can be changed now —
            stop the network to edit its mode, bridge or subnet.
          </div>
        )}

        <div className="wiz-form" style={{ padding: 20 }}>
          <div className="field">
            <span>Name</span>
            <input value={network.name} disabled className="mono" />
            <small className="field-hint">The network name is immutable.</small>
          </div>

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
                  disabled={submitting || !structuralAllowed}
                >
                  {m === 'bridge' ? 'bridge (LAN)' : m}
                </button>
              ))}
            </div>
          </div>

          {mode === 'bridge' ? (
            <label className="field">
              <span>Host bridge</span>
              {bridges && bridges.total > 0 ? (
                <select
                  value={bridgeName}
                  onChange={(e) => setBridgeName(e.target.value)}
                  disabled={submitting || !structuralAllowed}
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
                  disabled={submitting || !structuralAllowed}
                  className="mono"
                />
              )}
              {bridgeName && !bridgeOk && (
                <small className="field-error">Invalid interface name (max 16 chars)</small>
              )}
            </label>
          ) : (
            <>
              <label className="field">
                <span>Subnet (CIDR)</span>
                <input
                  value={cidr}
                  onChange={(e) => setCidr(e.target.value)}
                  placeholder="192.168.100.0/24"
                  disabled={submitting || !structuralAllowed}
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
                  disabled={submitting || !structuralAllowed}
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
          <button className="btn btn-primary btn-with-icon" onClick={() => void submit()} disabled={!valid || submitting}>
            <Pencil size={14} strokeWidth={2} aria-hidden />
            {submitting ? 'Saving…' : 'Save Changes'}
          </button>
        </footer>
      </div>
    </div>
  );
}
