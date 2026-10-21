import { existsSync } from 'node:fs';
import { randomUUID } from 'node:crypto';
import { readPrivate, privateDirectory } from './private-files.ts';
import { EncryptedFiles, SESSION_CONTEXT } from './crypto.ts';
import { parseConfig } from './config.ts';
import { validateSession, captureSession } from './session.ts';
import { LenovoBrowser } from './browser.ts';
import { collect, compare, withAuthenticationRecovery } from './collector.ts';
import { SnapshotStore } from './storage.ts';
import { Failure, safeFailure } from './errors.ts';

// Fixed container mounts, no supplier URL, credentials or source identifiers
// accepted in arguments/environment. The host wrapper holds the execution lock.
let browser: LenovoBrowser | undefined, timer: NodeJS.Timeout | undefined, store: SnapshotStore | undefined;
const abort = new AbortController(), attemptId = randomUUID();
let deadline = false;
let commitStarted = false;
const cancel = () => { abort.abort(); void browser?.close(); };
process.once('SIGTERM', cancel); process.once('SIGINT', cancel);
try {
  const config = parseConfig(JSON.parse(readPrivate('/credentials/config.json', 16384).toString('utf8')));
  const encrypted = new EncryptedFiles('/credentials/key.pem');
  privateDirectory('/state');
  const stored = existsSync('/state/session.enc');
  const session = validateSession(encrypted.read(stored ? '/state/session.enc' : '/credentials/session-import.enc', SESSION_CONTEXT, !stored), config.customerId);
  store = new SnapshotStore('/data', encrypted);
  timer = setTimeout(() => { deadline = true; cancel(); }, config.timeoutSeconds * 1000);
  browser = await LenovoBrowser.open(session, config.pageSize);
  if (abort.signal.aborted) throw new Failure('collection_cancelled');
  const client = browser;
  const snapshot = await withAuthenticationRecovery(async () => {
    await client.initialize();
    return collect(client, { customerId: config.customerId, timezone: config.timezone, now: new Date(),
      maxPages: config.maxPages, maxOrders: config.maxOrders, signal: abort.signal });
  }, () => client.renewSession(), abort.signal);
  const updatedSession = await captureSession(browser.context, config.customerId, session);
  // Check against accepted history before advancing credentials or facts.
  // commit repeats this check under the host execution lock.
  compare(store.last(), snapshot);
  encrypted.write('/state/session.enc', updatedSession, SESSION_CONTEXT);
  commitStarted = true;
  const result = store.commit(snapshot);
  console.log(JSON.stringify({ outcome: 'success', ...result, scope: snapshot.scope, coverage: snapshot.coverage,
    blockedRequests: browser.blockedCount(), normalRenewalResponses: browser.renewalCount(), sessionSaved: true, applicationIngestionEnabled: false }));
} catch (error) {
  const failure = deadline ? new Failure('network_error') : safeFailure(error);
  const summary = { outcome: 'failed', code: failure.code, retryAfterMs: failure.retryAfterMs,
    lastSuccessStatus: commitStarted ? 'inspect_last_success' : 'unchanged' };
  try { store?.attempt(summary, attemptId); } catch {}
  console.log(JSON.stringify(summary)); process.exitCode = 1;
} finally {
  if (timer) clearTimeout(timer);
  await browser?.close();
}
