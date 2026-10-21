import { Failure } from './errors.ts';

// Node 24 exposes the original primitive JSON token to a reviver. Preserve
// large integer identities and fractional decimal strings before Number can
// round them; ordinary bounded pagination/status integers stay numeric.
export function parseSourceJson(source: string): unknown {
  try {
    const parse = JSON.parse as (text: string, reviver: (key: string, value: unknown, context?: { source?: string }) => unknown) => unknown;
    return parse(source, (_key, value, context) => {
      if (typeof value !== 'number' || Number.isSafeInteger(value)) return value;
      if (!context?.source) throw new Failure('source_schema_invalid');
      if (!/^-?(0|[1-9][0-9]*)(\.[0-9]+)?$/.test(context.source)) throw new Failure('source_schema_invalid');
      return context.source;
    });
  } catch { throw new Failure('source_schema_invalid'); }
}
