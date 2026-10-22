import { lstatSync } from 'node:fs';
import { resolve, join } from 'node:path';
import { fileURLToPath } from 'node:url';
import { parseArgs } from 'node:util';
import { readPrivate, privateDirectory } from './private-files.ts';
import { EncryptedFiles, SESSION_CONTEXT } from './crypto.ts';
import { parseConfig } from './config.ts';
import { decodeScrapeyy, scrapeyyLenovoSession, SCRAPEYY_MAX_BYTES } from './scrapeyy-import.ts';
import { Failure, requireThat } from './errors.ts';

let bytes: Buffer | undefined, passphrase: Buffer | undefined;
try {
  requireThat(process.env.BWB_PROVISION_LOCK_HELD === '1', 'configuration_invalid');
  const { values } = parseArgs({ options: { 'private-dir': { type: 'string' }, input: { type: 'string' } }, strict: true });
  requireThat(values['private-dir'] && values.input, 'configuration_invalid');
  const root = resolve(values['private-dir']), input = resolve(values.input);
  const checkout = resolve(fileURLToPath(new URL('../../..', import.meta.url)));
  for (const path of [root, input]) {
    requireThat(path !== checkout && !path.startsWith(checkout + '/'), 'configuration_invalid');
    let ancestor = path;
    while (true) { requireThat(!lstatSync(ancestor).isSymbolicLink(), 'storage_error'); const next = resolve(ancestor, '..'); if (next === ancestor) break; ancestor = next; }
  }
  privateDirectory(root); privateDirectory(join(root, 'state'));
  const config = parseConfig(JSON.parse(readPrivate(join(root, 'credentials/config.json'), 16384).toString('utf8')));
  bytes = readPrivate(input, SCRAPEYY_MAX_BYTES);
  // The wrapper supplies a UTF-8 PIN/passphrase on stdin, never argv/environment.
  const chunks: Buffer[] = []; let length = 0;
  try {
    for await (const part of process.stdin) {
      const chunk = Buffer.from(part); chunks.push(chunk); length += chunk.length;
      requireThat(length <= 4096, 'configuration_invalid');
    }
    passphrase = Buffer.concat(chunks);
  } finally { for (const chunk of chunks) chunk.fill(0); }
  const session = scrapeyyLenovoSession(decodeScrapeyy(bytes, passphrase), config.customerId);
  const encrypted = new EncryptedFiles(join(root, 'credentials/key.pem'));
  encrypted.write(join(root, 'state/session-candidate.enc'), session, SESSION_CONTEXT);
  console.log(JSON.stringify({ outcome: 'candidate_imported', capturedAt: session.capturedAt,
    cookieCount: session.storageState.cookies.length, storageOriginCount: session.storageState.origins.length,
    activeSessionReplaced: false, accountVerified: false,
    next: 'Run run-isolated.py --mode verify-session with this private directory.' }));
} catch (error) {
  console.log(JSON.stringify({ outcome: 'failed', code: error instanceof Failure ? error.code : 'state_invalid', activeSessionReplaced: false }));
  process.exitCode = 1;
} finally { bytes?.fill(0); passphrase?.fill(0); }
