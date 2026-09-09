import { useRef, useState } from 'react';
import { api, unwrap, ApiError, uploadIso, type Iso } from '../api/client';
import { usePolling } from '../lib/hooks';

/** ISO Library card: list, upload (with progress) and delete. */
export function IsoLibrary() {
  const [uploading, setUploading] = useState<string | null>(null);
  const [pct, setPct] = useState(0);
  const [error, setError] = useState<string | null>(null);
  const [deleting, setDeleting] = useState<string | null>(null);
  const fileRef = useRef<HTMLInputElement>(null);
  const { data, refresh } = usePolling(async () => unwrap(api.GET('/storage/isos')), 10000);

  const pickFile = () => fileRef.current?.click();

  const onFile = async (file: File | undefined) => {
    if (!file) return;
    if (!file.name.toLowerCase().endsWith('.iso')) {
      setError('Only .iso files are accepted');
      return;
    }
    setError(null);
    setUploading(file.name);
    setPct(0);
    try {
      await uploadIso(file, setPct);
      await refresh();
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e));
    } finally {
      setUploading(null);
      if (fileRef.current) fileRef.current.value = '';
    }
  };

  const del = async (iso: Iso) => {
    if (!window.confirm(`Delete "${iso.fileName}" from the ISO library?`)) return;
    setDeleting(iso.id);
    setError(null);
    try {
      await unwrap(api.DELETE('/storage/isos/{id}', { params: { path: { id: iso.id } } }));
      await refresh();
    } catch (e) {
      setError(e instanceof ApiError ? `${e.code}: ${e.message}` : String(e));
    } finally {
      setDeleting(null);
    }
  };

  return (
    <div className="card iso-card">
      <div className="iso-head">
        <h3 className="card-title" style={{ margin: 0 }}>ISO Library</h3>
        <input
          ref={fileRef}
          type="file"
          accept=".iso"
          hidden
          onChange={(e) => void onFile(e.target.files?.[0])}
        />
        <button className="btn btn-primary" onClick={pickFile} disabled={uploading != null}>
          {uploading != null ? `Uploading ${pct}%` : '↑ Upload ISO'}
        </button>
      </div>

      {uploading != null && (
        <div className="iso-progress">
          <div className="iso-progress-fill" style={{ width: `${pct}%` }} />
        </div>
      )}
      {error && <div className="alert error" style={{ marginTop: 12 }}>{error}</div>}

      <table className="table">
        <thead>
          <tr>
            <th>File</th>
            <th>Size</th>
            <th>Uploaded</th>
            <th className="th-actions">Actions</th>
          </tr>
        </thead>
        <tbody>
          {(data?.items ?? []).map((iso) => (
            <tr key={iso.id}>
              <td className="mono">{iso.fileName}</td>
              <td>{formatSize(iso.sizeBytes)}</td>
              <td>{iso.uploadedAt ? new Date(iso.uploadedAt).toLocaleString() : '—'}</td>
              <td>
                <div className="actions">
                  <button
                    className="btn btn-danger"
                    disabled={deleting === iso.id || uploading != null}
                    onClick={() => void del(iso)}
                  >
                    Delete
                  </button>
                </div>
              </td>
            </tr>
          ))}
          {data?.total === 0 && (
            <tr>
              <td colSpan={4} className="iso-empty">
                No ISO images yet — upload one to use as install media when creating VMs.
              </td>
            </tr>
          )}
        </tbody>
      </table>
    </div>
  );
}

function formatSize(bytes: number): string {
  const units = ['B', 'KB', 'MB', 'GB', 'TB'];
  let v = bytes;
  let i = 0;
  while (v >= 1024 && i < units.length - 1) {
    v /= 1024;
    i++;
  }
  return `${v.toFixed(v >= 100 || i === 0 ? 0 : 1)} ${units[i]}`;
}
