import { createDecipheriv, pbkdf2Sync } from 'node:crypto';
import { Failure, requireThat } from './errors.ts';
import { array, object } from './model.ts';
import { validateSession } from './session.ts';
import type { Session } from './session.ts';

// The extension permits 20 MiB plaintext; base64 expands the encrypted file.
export const SCRAPEYY_MAX_BYTES = 28 * 1024 * 1024;
const account = 'https://account.lenovo.com';
const cookieOrigins = new Set([account, 'https://openapi.lenovo.com', 'https://www.lenovo.com',
  'https://passport.lenovo.com', 'https://bpsso.lenovo.com']);
const storageOrigins = new Set([account, 'https://passport.lenovo.com', 'https://bpsso.lenovo.com']);

function decode(value: unknown, size?: number): Buffer {
  requireThat(typeof value === 'string' && value.length <= SCRAPEYY_MAX_BYTES && /^[A-Za-z0-9+/]+={0,2}$/.test(value), 'state_invalid');
  const bytes = Buffer.from(value, 'base64');
  requireThat(bytes.toString('base64') === value && (size === undefined || bytes.length === size), 'state_invalid');
  return bytes;
}

// This is a file import only. No input can select a supplier request or a VM
// destination. The collector's independently pinned customer remains authoritative.
export function decodeScrapeyy(bytes: Buffer, passphrase?: Buffer): unknown {
  let key: Buffer | undefined, plain: Buffer | undefined;
  try {
    requireThat(bytes.length > 0 && bytes.length <= SCRAPEYY_MAX_BYTES, 'state_invalid');
    const input = object(JSON.parse(bytes.toString('utf8')));
    requireThat(input.version === 1, 'state_invalid');
    if (input.format === 'scrapeyy-session') return input;
    requireThat(input.format === 'scrapeyy-session-encrypted' && passphrase && passphrase.length > 0 && passphrase.length <= 4096, 'state_invalid');
    const kdf = object(input.kdf), cipher = object(input.cipher);
    requireThat(kdf.name === 'PBKDF2' && kdf.hash === 'SHA-256' && Number.isSafeInteger(kdf.iterations) &&
      Number(kdf.iterations) >= 100_000 && Number(kdf.iterations) <= 1_000_000, 'state_invalid');
    requireThat(cipher.name === 'AES-GCM' && cipher.tagLength === 128, 'state_invalid');
    const salt = decode(kdf.salt, 16), iv = decode(cipher.iv, 12), ciphertext = decode(input.ciphertext);
    requireThat(ciphertext.length > 16, 'state_invalid');
    key = pbkdf2Sync(passphrase, salt, Number(kdf.iterations), 32, 'sha256');
    const decipher = createDecipheriv('aes-256-gcm', key, iv);
    decipher.setAAD(Buffer.from('scrapeyy-session-encrypted:1')); decipher.setAuthTag(ciphertext.subarray(-16));
    plain = Buffer.concat([decipher.update(ciphertext.subarray(0, -16)), decipher.final()]);
    return JSON.parse(plain.toString('utf8'));
  } catch { throw new Failure('state_invalid'); }
  finally { key?.fill(0); plain?.fill(0); }
}

export function scrapeyyLenovoSession(value: unknown, customerId: string, now = Date.now()): Session {
  try {
    const bundle = object(value), source = object(bundle.source);
    requireThat(bundle.format === 'scrapeyy-session' && bundle.version === 1 && typeof bundle.createdAt === 'string', 'state_invalid');
    const capturedAt = Date.parse(bundle.createdAt);
    requireThat(Number.isFinite(capturedAt) && capturedAt <= now + 5 * 60_000, 'state_invalid');
    const origins = array(bundle.origins, 10);
    requireThat((origins.includes(account) || origins.includes('https://www.lenovo.com')) &&
      origins.every(o => typeof o === 'string' && cookieOrigins.has(o)), 'state_invalid');
    const cookies = array(bundle.cookies, 300).map(value => {
      const c = object(value);
      requireThat(typeof c.domain === 'string' && typeof c.hostOnly === 'boolean', 'state_invalid');
      const host = c.domain.replace(/^\./, '');
      requireThat(host === 'lenovo.com' || host.endsWith('.lenovo.com'), 'state_invalid');
      requireThat(/^[a-z0-9.-]+$/.test(host) && !host.startsWith('.') && !host.endsWith('.'), 'state_invalid');
      requireThat(origins.some(o => { const h = new URL(String(o)).hostname; return h === host || (!c.hostOnly && h.endsWith('.' + host)); }), 'state_invalid');
      requireThat((c.partitionKey === undefined || c.partitionKey === null) &&
        (c.firstPartyDomain === undefined || c.firstPartyDomain === ''), 'state_invalid');
      requireThat(typeof c.name === 'string' && c.name.length <= 500 && typeof c.value === 'string' && c.value.length <= 100_000 &&
        typeof c.path === 'string' && c.path.startsWith('/') && c.path.length <= 2000 &&
        typeof c.secure === 'boolean' && typeof c.httpOnly === 'boolean' && typeof c.session === 'boolean', 'state_invalid');
      requireThat(c.session || (typeof c.expirationDate === 'number' && Number.isFinite(c.expirationDate) && c.expirationDate > 0), 'state_invalid');
      const sites = new Map<string, 'None' | 'Lax' | 'Strict' | undefined>([
        ['no_restriction', 'None'], ['lax', 'Lax'], ['strict', 'Strict'], ['unspecified', undefined]
      ]);
      requireThat(typeof c.sameSite === 'string' && sites.has(c.sameSite), 'state_invalid');
      const sameSite = sites.get(c.sameSite);
      return { name: c.name, value: c.value, domain: c.hostOnly ? host : '.' + host, path: c.path,
        expires: c.session ? -1 : c.expirationDate as number, httpOnly: c.httpOnly, secure: c.secure, sameSite };
    });
    requireThat(cookies.length > 0, 'state_invalid');
    const storage = array(bundle.storage, 10).map(value => {
      const entry = object(value);
      requireThat(typeof entry.origin === 'string' && cookieOrigins.has(entry.origin) && origins.includes(entry.origin), 'state_invalid');
      const pairs = (value: unknown) => {
        const result = array(value, 300).map(value => {
          const pair = object(value);
          requireThat(typeof pair.name === 'string' && pair.name.length <= 500 && typeof pair.value === 'string' && pair.value.length <= 200_000, 'state_invalid');
          return { name: pair.name, value: pair.value };
        });
        requireThat(new Set(result.map(p => p.name)).size === result.length, 'state_invalid');
        return result;
      };
      return { origin: entry.origin, localStorage: pairs(entry.localStorage), sessionStorage: pairs(entry.sessionStorage) };
    });
    requireThat(new Set(storage.map(s => s.origin)).size === storage.length, 'state_invalid');
    const main = storage.find(s => s.origin === account);
    // Storefront exports can carry parent-domain portal cookies even when no
    // account tab was captured. Start missing account storage empty; the full
    // isolated verifier must still establish the configured account and scope.
    requireThat(typeof source.locale === 'string' && source.locale.length > 0 && source.locale.length < 40 &&
      typeof source.timezone === 'string' && source.timezone.length < 100, 'state_invalid');
    new Intl.Locale(source.locale); new Intl.DateTimeFormat('en-US', { timeZone: source.timezone });
    const session = validateSession({ schema: 1, capturedAt: new Date(capturedAt).toISOString(),
      storageState: { cookies, origins: storage.filter(s => storageOrigins.has(s.origin)).map(({ origin, localStorage }) => ({ origin, localStorage })) },
      sessionStorage: { origin: account, values: Object.fromEntries((main?.sessionStorage ?? []).map(p => [p.name, p.value])) },
      locale: source.locale, timezone: source.timezone, baseline: { customerId } }, customerId);
    requireThat(Buffer.byteLength(JSON.stringify(session)) <= 2 * 1024 * 1024, 'state_invalid');
    return session;
  } catch { throw new Failure('state_invalid'); }
}
