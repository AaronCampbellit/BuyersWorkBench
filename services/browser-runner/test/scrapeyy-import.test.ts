import test from 'node:test';
import assert from 'node:assert/strict';
import { generateKeyPairSync, randomBytes, webcrypto } from 'node:crypto';
import { mkdtempSync, mkdirSync, writeFileSync, readFileSync, rmSync, statSync, symlinkSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { fileURLToPath } from 'node:url';
import { spawnSync } from 'node:child_process';
import { decodeScrapeyy, scrapeyyLenovoSession } from '../src/scrapeyy-import.ts';
import { EncryptedFiles, SESSION_CONTEXT } from '../src/crypto.ts';
import type { Session } from '../src/session.ts';

const secret = 'FICTIONAL PORTABLE PASSPHRASE';
test('accepts golden fixtures produced by Scrapeyy encodeSession v1', () => {
  const fixture = (name: string) => readFileSync(fileURLToPath(new URL('./fixtures/scrapeyy/' + name + '.json', import.meta.url)));
  const plaintext = decodeScrapeyy(fixture('plaintext'));
  assert.deepEqual(decodeScrapeyy(fixture('encrypted'), Buffer.from('1234')), plaintext);
  const session = scrapeyyLenovoSession(plaintext, 'PINNED-CUSTOMER');
  assert.equal(session.storageState.cookies[0]!.value, 'not-a-real-session');
});
function payload() {
  return { format: 'scrapeyy-session', version: 1, createdAt: '2026-10-20T18:00:00.000Z',
    source: { browser: 'chrome', locale: 'en-US', timezone: 'America/Chicago' },
    origins: ['https://account.lenovo.com', 'https://passport.lenovo.com'],
    cookies: [
      { name: 'FICT-SESSION', value: 'FICTIONAL-SESSION-COOKIE', domain: '.lenovo.com', path: '/us/lenovopro',
        hostOnly: false, secure: true, httpOnly: true, sameSite: 'no_restriction', session: false, expirationDate: 1792607400, storeId: '0' },
      { name: 'FICT-HOST', value: 'FICTIONAL-HOST-COOKIE', domain: 'account.lenovo.com', path: '/',
        hostOnly: true, secure: true, httpOnly: true, sameSite: 'unspecified', session: true, storeId: '0' }
    ],
    storage: [{ origin: 'https://account.lenovo.com', localStorage: [{ name: 'provider', value: 'FICTIONAL-LOCAL-STORAGE' }],
      sessionStorage: [{ name: '__proto__', value: 'FICTIONAL-SESSION-STORAGE' }] }], warnings: [] };
}
async function envelope(value: unknown) {
  // The browser uses WebCrypto. Decrypting this with Node verifies tag layout,
  // UTF-8 passphrase handling, AAD, and PBKDF2 interoperability.
  const salt = randomBytes(16), iv = randomBytes(12);
  const base = await webcrypto.subtle.importKey('raw', Buffer.from(secret), 'PBKDF2', false, ['deriveKey']);
  const key = await webcrypto.subtle.deriveKey({ name: 'PBKDF2', salt, iterations: 600_000, hash: 'SHA-256' }, base,
    { name: 'AES-GCM', length: 256 }, false, ['encrypt']);
  const bytes = await webcrypto.subtle.encrypt({ name: 'AES-GCM', iv, additionalData: Buffer.from('scrapeyy-session-encrypted:1'), tagLength: 128 },
    key, Buffer.from(JSON.stringify(value)));
  return { format: 'scrapeyy-session-encrypted', version: 1,
    kdf: { name: 'PBKDF2', hash: 'SHA-256', iterations: 600_000, salt: salt.toString('base64') },
    cipher: { name: 'AES-GCM', iv: iv.toString('base64'), tagLength: 128 }, ciphertext: Buffer.from(bytes).toString('base64') };
}
test('portable WebCrypto and plaintext imports preserve cookie scope, original expiry and storage', async () => {
  const original = payload();
  original.cookies = original.cookies.map(cookie => ({ ...cookie, partitionKey: null })); // Firefox: unpartitioned.
  original.origins.push('https://www.lenovo.com');
  original.storage.push({ origin: 'https://www.lenovo.com', localStorage: [{ name: 'unused', value: 'FICTIONAL-UNUSED' }], sessionStorage: [] });
  const encrypted = await envelope(original);
  for (const file of [original, encrypted]) {
    const decoded = decodeScrapeyy(Buffer.from(JSON.stringify(file)), Buffer.from(secret));
    const session = scrapeyyLenovoSession(decoded, 'PINNED-CUSTOMER');
    assert.equal(session.baseline.customerId, 'PINNED-CUSTOMER');
    assert.equal(session.capturedAt, original.createdAt);
    assert.deepEqual(session.storageState.cookies.map(c => [c.domain, c.path, c.expires, c.sameSite]),
      [['.lenovo.com', '/us/lenovopro', 1792607400, 'None'], ['account.lenovo.com', '/', -1, undefined]]);
    assert.equal(session.storageState.cookies[0]!.httpOnly, true);
    assert.equal(session.storageState.origins.length, 1, 'only reviewed storage origins are restored');
    assert.equal(Object.getPrototypeOf(session.sessionStorage.values), Object.prototype);
    assert.equal(session.sessionStorage.values['__proto__'], 'FICTIONAL-SESSION-STORAGE');
  }
});
test('wrong PIN, tampering and unbounded encryption settings fail without private diagnostics', async () => {
  const encrypted = await envelope(payload());
  const reject = (value: unknown, passphrase = secret) => assert.throws(() => decodeScrapeyy(Buffer.from(JSON.stringify(value)), Buffer.from(passphrase)),
    { message: 'state_invalid' });
  reject(encrypted, 'WRONG PIN');
  reject({ ...encrypted, kdf: { ...encrypted.kdf, iterations: 10_000_000_000 } });
  reject({ ...encrypted, cipher: { ...encrypted.cipher, tagLength: 32 } });
  reject({ ...encrypted, ciphertext: 'A' + encrypted.ciphertext.slice(1, -4) + 'AAAA' });
  reject({ ...encrypted, version: 2 });
});
test('storefront exports start missing account storage empty and retain only applicable cookies', () => {
  const original = payload();
  original.origins = ['https://www.lenovo.com'];
  original.cookies = [original.cookies[0]!].map(cookie => ({ ...cookie, partitionKey: null }));
  original.storage = [{ origin: 'https://www.lenovo.com', localStorage: [{ name: 'storefront', value: 'FICTIONAL-STOREFRONT' }], sessionStorage: [] }];
  const session = scrapeyyLenovoSession(original, 'PINNED-CUSTOMER');
  assert.equal(session.storageState.cookies[0]!.domain, '.lenovo.com');
  assert.deepEqual(session.storageState.origins, []);
  assert.deepEqual(session.sessionStorage, { origin: 'https://account.lenovo.com', values: {} });
  assert.ok(!JSON.stringify(session).includes('FICTIONAL-STOREFRONT'));
  original.cookies[0]!.hostOnly = true; original.cookies[0]!.domain = 'account.lenovo.com';
  assert.throws(() => scrapeyyLenovoSession(original, 'PINNED-CUSTOMER'), { message: 'state_invalid' });
});
test('Lenovo staging rejects unrelated origins, cookie widening, partitioning and malformed state', () => {
  const corrupt = (edit: (value: any) => void) => { const value = payload(); edit(value); assert.throws(() => scrapeyyLenovoSession(value, 'PINNED-CUSTOMER'), { message: 'state_invalid' }); };
  corrupt(v => { v.origins.push('https://unrelated.invalid'); });
  corrupt(v => { v.cookies[0].domain = '.lenovo.com.unrelated.invalid'; });
  corrupt(v => { v.cookies[0].domain = 'unselected.lenovo.com'; v.cookies[0].hostOnly = true; });
  corrupt(v => { v.cookies[0].partitionKey = { topLevelSite: 'https://lenovo.com' }; });
  corrupt(v => { v.cookies[0].firstPartyDomain = 'lenovo.com'; });
  corrupt(v => { v.cookies[0].sameSite = '__proto__'; });
  corrupt(v => { delete v.cookies[0].expirationDate; });
  corrupt(v => { v.storage[0].origin = 'https://unrelated.invalid'; });
  corrupt(v => { v.storage.push(v.storage[0]); });
  corrupt(v => { v.storage[0].localStorage.push(v.storage[0].localStorage[0]); });
  corrupt(v => { v.createdAt = 'INVALID TIMESTAMP'; });
  corrupt(v => { v.createdAt = '2100-01-01T00:00:00Z'; });
});
test('file import re-encrypts a candidate locally, rejects unsafe files and preserves active state/history', { skip: process.getuid?.() === 0 }, async () => {
  const root = mkdtempSync(join(tmpdir(), 'bwb-scrapeyy-test-'));
  for (const folder of ['credentials', 'state', 'data']) mkdirSync(join(root, folder), { mode: 0o700 });
  const key = generateKeyPairSync('rsa', { modulusLength: 3072 }).privateKey;
  writeFileSync(join(root, 'credentials/key.pem'), key.export({ type: 'pkcs8', format: 'pem' }), { mode: 0o600 });
  writeFileSync(join(root, 'credentials/config.json'), JSON.stringify({ schema: 1, customerId: 'PINNED-CUSTOMER', timezone: 'America/Chicago' }), { mode: 0o600 });
  writeFileSync(join(root, 'state/session.enc'), 'EXISTING-ACTIVE', { mode: 0o600 });
  writeFileSync(join(root, 'data/last-success.json'), 'EXISTING-HISTORY', { mode: 0o600 });
  const input = join(root, 'borrowed.json'), script = fileURLToPath(new URL('../import-scrapeyy.py', import.meta.url));
  const run = (path = input, passphrase?: string) => spawnSync('python3', [script, '--private-dir', root, '--input', path,
    ...(passphrase === undefined ? [] : ['--passphrase-stdin'])], { input: passphrase ?? '', encoding: 'utf8' });
  try {
    for (const file of [payload(), await envelope(payload())]) {
      writeFileSync(input, JSON.stringify(file), { mode: 0o600 });
      const result = run(input, secret);
      assert.equal(result.status, 0, result.stdout + result.stderr);
      assert.equal(JSON.parse(result.stdout).accountVerified, false);
      assert.ok(!result.stdout.includes('FICTIONAL-SESSION-COOKIE'));
      assert.equal(result.stderr, '');
      const path = join(root, 'state/session-candidate.enc');
      assert.equal(statSync(path).mode & 0o777, 0o600);
      const local = new EncryptedFiles(join(root, 'credentials/key.pem')).read(path, SESSION_CONTEXT) as Session;
      assert.equal(local.storageState.cookies[0]!.value, 'FICTIONAL-SESSION-COOKIE');
    }
    const storefront = payload();
    storefront.origins = ['https://www.lenovo.com']; storefront.cookies = [storefront.cookies[0]!];
    storefront.storage = [{ origin: 'https://www.lenovo.com', localStorage: [], sessionStorage: [] }];
    writeFileSync(input, JSON.stringify(storefront), { mode: 0o600 });
    const cookieOnly = run();
    assert.equal(cookieOnly.status, 0, cookieOnly.stdout + cookieOnly.stderr);
    assert.equal(JSON.parse(cookieOnly.stdout).accountStorageCaptured, false);
    assert.equal(JSON.parse(cookieOnly.stdout).renewalCookiesComplete, false);
    writeFileSync(input, JSON.stringify(await envelope(payload())), { mode: 0o600 });
    const previous = readFileSync(join(root, 'state/session-candidate.enc'));
    assert.equal(run(input, 'WRONG PIN').status, 1);
    assert.equal(JSON.parse(run().stdout).code, 'passphrase_required');
    const wrong = payload(); wrong.origins.push('https://unrelated.invalid');
    writeFileSync(input, JSON.stringify(wrong), { mode: 0o600 });
    assert.equal(run().status, 1);
    symlinkSync(input, join(root, 'symlink.json'));
    assert.equal(JSON.parse(run(join(root, 'symlink.json')).stdout).code, 'unsafe_private_path');
    writeFileSync(join(root, 'public.json'), JSON.stringify(payload()), { mode: 0o644 });
    assert.equal(JSON.parse(run(join(root, 'public.json')).stdout).code, 'unsafe_input_file');
    assert.deepEqual(readFileSync(join(root, 'state/session-candidate.enc')), previous);
    assert.equal(readFileSync(join(root, 'state/session.enc'), 'utf8'), 'EXISTING-ACTIVE');
    assert.equal(readFileSync(join(root, 'data/last-success.json'), 'utf8'), 'EXISTING-HISTORY');
  } finally { rmSync(root, { recursive: true, force: true }); }
});
