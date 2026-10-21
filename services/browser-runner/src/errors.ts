export const codes = ['authentication_required', 'challenge_required', 'permission_denied', 'account_mismatch',
  'source_schema_invalid', 'incomplete_scope', 'network_error', 'rate_limited', 'source_unavailable',
  'configuration_invalid', 'state_invalid', 'storage_error', 'collection_cancelled'] as const;
export type FailureCode = typeof codes[number];

// Never retain a provider body, URL, OS error or cause in an operational error.
export class Failure extends Error {
  readonly code: FailureCode;
  readonly retryAfterMs: number;
  constructor(code: FailureCode, retryAfterMs = 0) {
    super(code);
    this.name = 'Failure'; this.code = code;
    this.retryAfterMs = Math.max(0, Math.min(300_000, retryAfterMs));
  }
}
export function safeFailure(error: unknown, fallback: FailureCode = 'source_unavailable'): Failure {
  return error instanceof Failure ? error : new Failure(fallback);
}
export function requireThat(value: unknown, code: FailureCode = 'source_schema_invalid'): asserts value {
  if (!value) throw new Failure(code);
}
export function retryable(error: Failure): boolean {
  return ['network_error', 'rate_limited', 'source_unavailable'].includes(error.code);
}
