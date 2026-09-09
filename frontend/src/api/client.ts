import createClient from 'openapi-fetch';
import type { components, paths } from './schema';

/**
 * Typed API client generated from the OpenAPI contract.
 * There is no hand-written fetch layer: every call is derived from
 * docs/api/openapi.yaml (single source of truth).
 */
export const api = createClient<paths>({ baseUrl: '/api/v1' });

export type VirtualMachine = components['schemas']['VirtualMachine'];
export type Host = components['schemas']['Host'];
export type Capabilities = components['schemas']['Capabilities'];
export type StoragePool = components['schemas']['StoragePool'];
export type Network = components['schemas']['Network'];
export type Iso = components['schemas']['Iso'];

/** Uniform error handling for the standard error envelope. */
export class ApiError extends Error {
  constructor(
    public code: string,
    message: string,
    public status: number,
  ) {
    super(message);
  }
}

export async function unwrap<T, E = unknown>(
  promise: Promise<{ data?: T; error?: E; response: Response }>,
): Promise<T> {
  const { data, error, response } = await promise;
  if (error) {
    const err = error as { error?: { code?: string; message?: string } };
    throw new ApiError(
      err?.error?.code ?? 'UNKNOWN_ERROR',
      err?.error?.message ?? 'Unexpected error',
      response.status,
    );
  }
  return data as T;
}

/** Upload com progresso (fetch não expõe progresso de upload). */
export function uploadIso(
  file: File,
  onProgress?: (pct: number) => void,
): Promise<void> {
  return new Promise((resolve, reject) => {
    const xhr = new XMLHttpRequest();
    xhr.open('POST', '/api/v1/storage/isos');
    xhr.upload.onprogress = (e) => {
      if (e.lengthComputable) onProgress?.(Math.round((e.loaded / e.total) * 100));
    };
    xhr.onload = () => {
      if (xhr.status === 201) return resolve();
      let msg = `HTTP ${xhr.status}`;
      try {
        const body = JSON.parse(xhr.responseText);
        msg = body?.error?.code
          ? `${body.error.code}: ${body.error.message}`
          : (body?.error?.message ?? msg);
      } catch { /* keep default */ }
      reject(new Error(msg));
    };
    xhr.onerror = () => reject(new Error('Network error during upload'));
    const fd = new FormData();
    fd.append('file', file);
    xhr.send(fd);
  });
}
