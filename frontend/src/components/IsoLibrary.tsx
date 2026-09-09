import { useRef, useState } from 'react';
import { api, unwrap, ApiError, uploadIso, type Iso } from '../api/client';
import { usePolling } from '../lib/hooks';
import { useToast } from './Toast';
import { Disc, Upload, Trash2, TriangleAlert } from 'lucide-react';
import { ActionMenu, ConfirmDialog, type MenuItem } from './ui';

/** ISO Library card: list, upload (with progress) and delete. */
export function IsoLibrary() {
  const toast = useToast();
  const [confirmDel, setConfirmDel] = useState<Iso | null>(null);
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
      toast.push('success', `${file.name}: upload complete`);
      await refresh();
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e));
      toast.push('error', `${file.name}: upload failed`);
    } finally {
      setUploading(null);
      if (fileRef.current) fileRef.current.value = '';
    }
  };

  const del = async (iso: Iso) => {
    setDeleting(iso.id);
    setError(null);
    try {
      await unwrap(api.DELETE('/storage/isos/{id}', { params: { path: { id: iso.id } } }));
      toast.push('success', `${iso.fileName}: deleted`);
      await refresh();
    } catch (e) {
      const msg = e instanceof ApiError ? `${e.code}: ${e.message}` : String(e);
      setError(msg);
      toast.push('error', `${iso.fileName}: ${msg}`);
    } finally {
      setDeleting(null);
      setConfirmDel(null);
    }
  };

  return (
    <div className="card iso-card">
      <div className="iso-head">
        <h3 className="card-title" style={{ margin: 0, display: 'flex', alignItems: 'center', gap: 9 }}>
          <Disc size={15} className="ic ic-purple" aria-hidden /> ISO Library
        </h3>
        <input
          ref={fileRef}
          type="file"
          accept=".iso"
          hidden
          onChange={(e) => void onFile(e.target.files?.[0])}
        />
        <button className="btn btn-primary btn-with-icon" onClick={pickFile} disabled={uploading != null}>
          <Upload size={14} strokeWidth={2} aria-hidden />
          {uploading != null ? `Uploading ${pct}%` : 'Upload ISO'}
        </button>
      </div>

      {uploading != null && (
        <div className="iso-progress">
          <div className="iso-progress-fill" style={{ width: `${pct}%` }} />
        </div>
      )}
      {error && (
        <div className="alert error" style={{ marginTop: 12, display: 'flex', alignItems: 'center', gap: 9 }}>
          <TriangleAlert size={14} aria-hidden /> {error}
        </div>
      )}

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
              <td className="td-actions">
                <ActionMenu
                  items={[
                    {
                      label: 'Delete',
                      icon: <Trash2 size={14} strokeWidth={1.75} aria-hidden />,
                      danger: true,
                      disabled: deleting === iso.id || uploading != null,
                      onSelect: () => setConfirmDel(iso),
                    },
                  ] as MenuItem[]}
                  label={`Actions for ${iso.fileName}`}
                />
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

      <ConfirmDialog
        open={confirmDel != null}
        title={`Delete "${confirmDel?.fileName ?? ''}"?`}
        message={<>The ISO file will be permanently removed from the library. VMs currently using it as install media will be affected.</>}
        confirmLabel="Delete"
        busy={confirmDel != null && deleting === confirmDel.id}
        onCancel={() => setConfirmDel(null)}
        onConfirm={() => confirmDel && void del(confirmDel)}
      />
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
