import { Failure, requireThat } from './errors.ts';
import { identifier, integer, object } from './model.ts';

export interface Config { schema: 1; customerId: string; timezone: string; pageSize: number; maxPages: number; maxOrders: number; timeoutSeconds: number }
export function parseConfig(value: unknown): Config {
  try {
    const raw = object(value);
    requireThat(raw.schema === 1 && Object.keys(raw).every(k => ['schema', 'customerId', 'timezone', 'pageSize', 'maxPages', 'maxOrders', 'timeoutSeconds'].includes(k)));
    const customerId = identifier(raw.customerId);
    requireThat(typeof raw.timezone === 'string' && raw.timezone.length <= 100);
    new Intl.DateTimeFormat('en-US', { timeZone: raw.timezone });
    const pageSize = integer(raw.pageSize ?? 25, 100), maxPages = integer(raw.maxPages ?? 100, 100), maxOrders = integer(raw.maxOrders ?? 1000, 10000), timeoutSeconds = integer(raw.timeoutSeconds ?? 300, 600);
    requireThat(pageSize > 0 && maxPages > 0 && maxOrders > 0 && timeoutSeconds >= 30);
    return { schema: 1, customerId, timezone: raw.timezone, pageSize, maxPages, maxOrders, timeoutSeconds };
  } catch { throw new Failure('configuration_invalid'); }
}
