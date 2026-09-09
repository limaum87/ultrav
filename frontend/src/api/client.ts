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
