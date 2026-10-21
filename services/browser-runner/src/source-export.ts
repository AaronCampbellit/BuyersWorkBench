import type { Page } from 'playwright';

// Serialized as a function and executed by the operator's source-device MCP.
// Keep every dependency local or a standard Node/WebCrypto/browser API. Only
// ciphertext and counts leave that device, never plaintext cookies/storage.
export async function captureScopedSession(page: Page, expectedCustomer: string, publicKeyBase64: string) {
  let cleartext: Uint8Array | undefined, rawKey: ArrayBuffer | undefined;
  let stage = 'source_url';
  try {
    const source = new URL(page.url());
    const prefix = '/us/lenovopro/en/account/order/';
    if (source.origin !== 'https://account.lenovo.com' || ![prefix + 'list.html', prefix + 'detail.html'].includes(source.pathname))
      return { ok: false, code: 'select_authenticated_lenovo_tab' };
    const detail = source.pathname.endsWith('/detail.html');
    stage = 'source_reload';
    const pending = page.waitForResponse(response => {
      const u = new URL(response.url());
      return u.origin === 'https://openapi.lenovo.com' && response.request().method() === 'GET' &&
        u.pathname === '/us/lenovopro/en/ofp/' + (detail ? 'order/authDetail' : 'auth/mobileOrder/list');
    }, { timeout: 25000 }).catch(() => null);
    await page.reload({ waitUntil: 'domcontentloaded', timeout: 30000 });
    const response = await pending;
    if (!response || response.status() !== 200) return { ok: false, code: 'authenticated_source_required' };
    stage = 'source_account';
    const body = await response.json();
    const customers: unknown[] = detail ? [body.data?.customerId] : (body.data?.page?.datas ?? []).map((r: { customerId: unknown }) => r.customerId);
    if (body.status !== 200 || !customers.length || customers.some(value => typeof value !== 'string' && typeof value !== 'number' || String(value) !== expectedCustomer))
      return { ok: false, code: 'source_account_unverified' };
    stage = 'source_storage';
    const storage = await page.evaluate(() => ({ origin: location.origin,
      localStorage: Object.keys(localStorage).map(name => ({ name, value: localStorage.getItem(name)! })),
      sessionStorage: Object.fromEntries(Object.keys(sessionStorage).map(name => [name, sessionStorage.getItem(name)!])),
      locale: navigator.language, timezone: Intl.DateTimeFormat().resolvedOptions().timeZone }));
    stage = 'source_cookies';
    const cookies = await page.context().cookies(['https://account.lenovo.com/', 'https://openapi.lenovo.com/',
      'https://www.lenovo.com/', 'https://passport.lenovo.com/', 'https://bpsso.lenovo.com/']);
    if (cookies.some(c => !(c.domain === 'lenovo.com' || c.domain === '.lenovo.com' || c.domain.endsWith('.lenovo.com'))))
      return { ok: false, code: 'unexpected_cookie_scope' };
    const capturedAt = new Date().toISOString();
    const session = { schema: 1, capturedAt, storageState: { cookies, origins: [{ origin: storage.origin, localStorage: storage.localStorage }] },
      sessionStorage: { origin: storage.origin, values: storage.sessionStorage }, locale: storage.locale, timezone: storage.timezone,
      baseline: { customerId: expectedCustomer } };
    stage = 'source_encryption';
    const plain = Buffer.from(JSON.stringify(session));
    cleartext = plain;
    if (plain.length > 2 * 1024 * 1024) { plain.fill(0); return { ok: false, code: 'source_state_too_large' }; }
    const wrappingKey = await crypto.subtle.importKey('spki', Buffer.from(publicKeyBase64, 'base64'), { name: 'RSA-OAEP', hash: 'SHA-256' }, false, ['encrypt']);
    const key = await crypto.subtle.generateKey({ name: 'AES-GCM', length: 256 }, true, ['encrypt']);
    const iv = crypto.getRandomValues(new Uint8Array(12)), context = 'buyersworkbench:lenovo:session:v1';
    const ciphertext = await crypto.subtle.encrypt({ name: 'AES-GCM', iv, additionalData: Buffer.from(context) }, key, plain); plain.fill(0);
    const raw = await crypto.subtle.exportKey('raw', key);
    rawKey = raw;
    const wrapped = await crypto.subtle.encrypt({ name: 'RSA-OAEP' }, wrappingKey, raw); new Uint8Array(raw).fill(0);
    return { ok: true, capturedAt, cookieCount: cookies.length, encryptedAtSource: true,
      envelope: { schema: 1, algorithm: 'RSA-OAEP-SHA256+A256GCM', context,
        iv: Buffer.from(iv).toString('base64'), ciphertext: Buffer.from(ciphertext).toString('base64'), wrappedKey: Buffer.from(wrapped).toString('base64') } };
  } catch { return { ok: false, code: 'source_capture_failed', stage }; }
  finally { cleartext?.fill(0); if (rawKey) new Uint8Array(rawKey).fill(0); }
}
