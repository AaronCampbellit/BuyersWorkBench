import type { BrowserContext } from 'playwright';
import { requireThat } from './errors.ts';
import { array, object, text } from './model.ts';

type StorageState = Awaited<ReturnType<BrowserContext['storageState']>>;
export interface Session {
  schema: 1; capturedAt: string; storageState: StorageState;
  sessionStorage: { origin: string; values: Record<string, string> };
  locale: string; timezone: string; baseline: { customerId: string };
}
const origins = new Set(['https://account.lenovo.com', 'https://passport.lenovo.com', 'https://bpsso.lenovo.com']);
export function validateSession(value: unknown, customerId: string): Session {
  const session = object(value), baseline = object(session.baseline), storage = object(session.storageState), ss = object(session.sessionStorage);
  requireThat(session.schema === 1 && baseline.customerId === customerId, 'account_mismatch');
  for (const v of array(storage.cookies, 300)) {
    const cookie = object(v), domain = text(cookie.domain)!;
    requireThat(domain === 'lenovo.com' || domain === '.lenovo.com' || domain.endsWith('.lenovo.com'), 'state_invalid');
    requireThat(typeof cookie.value === 'string' && cookie.value.length <= 100_000 && typeof cookie.name === 'string' && typeof cookie.path === 'string', 'state_invalid');
  }
  for (const v of array(storage.origins, 10)) {
    const origin = object(v); requireThat(origins.has(String(origin.origin)), 'state_invalid');
    for (const entry of array(origin.localStorage, 300)) { const x = object(entry); requireThat(typeof x.name === 'string' && typeof x.value === 'string' && x.value.length <= 200_000, 'state_invalid'); }
  }
  requireThat(ss.origin === 'https://account.lenovo.com', 'state_invalid');
  const values = object(ss.values);
  requireThat(Object.keys(values).length <= 300 && Object.entries(values).every(([k, v]) => k.length <= 500 && typeof v === 'string' && v.length <= 200_000), 'state_invalid');
  requireThat(typeof session.locale === 'string' && session.locale.length < 40 && typeof session.timezone === 'string' && session.timezone.length < 100, 'state_invalid');
  return session as unknown as Session;
}
export async function captureSession(context: BrowserContext, customerId: string, original: Session): Promise<Session> {
  const state = await context.storageState();
  state.cookies = state.cookies.filter(c => c.domain === 'lenovo.com' || c.domain === '.lenovo.com' || c.domain.endsWith('.lenovo.com'));
  state.origins = state.origins.filter(o => origins.has(o.origin));
  const page = context.pages().find(p => p.url().startsWith('https://account.lenovo.com/'));
  const sessionStorage = page ? await page.evaluate(() => ({ origin: location.origin,
    values: Object.fromEntries(Object.keys(window.sessionStorage).map(k => [k, window.sessionStorage.getItem(k)!])) })) : original.sessionStorage;
  return validateSession({ schema: 1, capturedAt: new Date().toISOString(), storageState: state, sessionStorage,
    locale: original.locale, timezone: original.timezone, baseline: { customerId } }, customerId);
}
