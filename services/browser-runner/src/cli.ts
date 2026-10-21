import { existsSync } from 'node:fs';
import { randomUUID } from 'node:crypto';
import { createHash } from 'node:crypto';
import { readPrivate, privateDirectory, atomicPrivate } from './private-files.ts';
import { EncryptedFiles, SESSION_CONTEXT } from './crypto.ts';
import { parseConfig } from './config.ts';
import { validateSession, captureSession } from './session.ts';
import { LenovoBrowser } from './browser.ts';
import { collect, compare } from './collector.ts';
import { collectWithRenewal } from './session-flow.ts';
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
  const mode = process.argv[2] ?? 'collect';
  if (!['collect', 'renew', 'verify-session'].includes(mode) || process.argv.length > 3) throw new Failure('configuration_invalid');
  const config = parseConfig(JSON.parse(readPrivate('/credentials/config.json', 16384).toString('utf8')));
  const encrypted = new EncryptedFiles('/credentials/key.pem');
  privateDirectory('/state');
  const stored = existsSync('/state/session.enc'), candidate = mode === 'verify-session';
  const session = validateSession(encrypted.read(candidate ? '/state/session-candidate.enc' : stored ? '/state/session.enc' : '/credentials/session-import.enc', SESSION_CONTEXT, !stored && !candidate), config.customerId);
  store = new SnapshotStore('/data', encrypted);
  timer = setTimeout(() => { deadline = true; cancel(); }, config.timeoutSeconds * 1000);
  browser = await LenovoBrowser.open(session, config.pageSize);
  if (abort.signal.aborted) throw new Failure('collection_cancelled');
  const client = browser;
  const flow = await collectWithRenewal({ initialize: () => client.initialize(), renew: () => client.renewSession(),
    collect: () => collect(client, { customerId: config.customerId, timezone: config.timezone, now: new Date(),
      maxPages: config.maxPages, maxOrders: config.maxOrders, signal: abort.signal }),
    capture: () => captureSession(client.context, config.customerId, session),
    renewalObserved: () => client.renewalCount() > 0 || client.authDiagnostics().normalSsoInvocations > 0
  }, session, mode === 'renew', new Date(), abort.signal);
  const snapshot = flow.snapshot, updatedSession = flow.session;
  // Check against accepted history before advancing credentials or facts.
  // commit repeats this check under the host execution lock.
  compare(store.last(), snapshot);
  encrypted.write('/state/session.enc', updatedSession, SESSION_CONTEXT);
  commitStarted = true;
  const result = store.commit(snapshot);
  atomicPrivate('/data/session-health.json', JSON.stringify({ schema: 1, capturedAt: updatedSession.capturedAt,
    runId: result.runId, stateDigest: createHash('sha256').update(readPrivate('/state/session.enc')).digest('hex'),
    clocks: flow.renewal.after, renewal: flow.renewal }));
  console.log(JSON.stringify({ outcome: 'success', ...result, scope: snapshot.scope, coverage: snapshot.coverage,
    blockedRequests: browser.blockedCount(), normalRenewalResponses: browser.renewalCount(), authDiagnostics: browser.authDiagnostics(),
    renewal: flow.renewal, sessionSaved: true, candidatePromoted: candidate, applicationIngestionEnabled: false }));
} catch (error) {
  const failure = deadline ? new Failure('network_error') : safeFailure(error);
  const summary = { outcome: 'failed', code: failure.code, retryAfterMs: failure.retryAfterMs,
    authDiagnostics: browser?.authDiagnostics() ?? null,
    lastSuccessStatus: commitStarted ? 'inspect_last_success' : 'unchanged' };
  try { store?.attempt(summary, attemptId); } catch {}
  console.log(JSON.stringify(summary)); process.exitCode = 1;
} finally {
  if (timer) clearTimeout(timer);
  await browser?.close();
}
