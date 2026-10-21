import { createPublicKey, randomUUID } from 'node:crypto';
import { unlinkSync, lstatSync } from 'node:fs';
import { resolve, join } from 'node:path';
import { fileURLToPath } from 'node:url';
import { parseArgs } from 'node:util';
import { readPrivate, atomicPrivate, privateDirectory } from './private-files.ts';
import { EncryptedFiles, SESSION_CONTEXT } from './crypto.ts';
import { parseConfig } from './config.ts';
import { validateSession } from './session.ts';
import { captureScopedSession } from './source-export.ts';
import { LocalMcp } from './mcp-client.ts';
import { Failure, safeFailure, requireThat } from './errors.ts';

let stage = 'configuration', sourceFailure: string | undefined, sourceStage: string | undefined;
let mcp: LocalMcp | undefined;
try {
  requireThat(process.env.BWB_PROVISION_LOCK_HELD === '1', 'configuration_invalid');
  const { values } = parseArgs({ options: { 'private-dir': { type: 'string' }, 'mcp-url': { type: 'string', default: 'http://localhost:8931/mcp' } }, strict: true });
  requireThat(values['private-dir'], 'configuration_invalid');
  const root = resolve(values['private-dir']), checkout = resolve(fileURLToPath(new URL('../../..', import.meta.url)));
  requireThat(root !== checkout && !root.startsWith(checkout + '/'), 'configuration_invalid');
  let ancestor = root;
  while (true) { requireThat(!lstatSync(ancestor).isSymbolicLink(), 'storage_error'); const next = resolve(ancestor, '..'); if (next === ancestor) break; ancestor = next; }
  privateDirectory(root); const state = join(root, 'state'); privateDirectory(state);
  const config = parseConfig(JSON.parse(readPrivate(join(root, 'credentials/config.json'), 16384).toString('utf8')));
  const keyFile = join(root, 'credentials/key.pem'), encrypted = new EncryptedFiles(keyFile);
  const key = readPrivate(keyFile, 16384); let publicKey: string;
  try { publicKey = createPublicKey(key).export({ type: 'spki', format: 'der' }).toString('base64'); } finally { key.fill(0); }
  stage = 'connecting_mcp';
  mcp = new LocalMcp(values['mcp-url']!); const tool = await mcp.initialize();
  const code = 'async (page) => { const capture = ' + captureScopedSession.toString() + '; return capture(page, ' + JSON.stringify(config.customerId) + ', ' + JSON.stringify(publicKey) + '); }';
  stage = 'capturing_selected_source_tab';
  const result = await mcp.run(tool, code) as Record<string, unknown>;
  if (typeof result.code === 'string' && ['select_authenticated_lenovo_tab', 'authenticated_source_required',
    'source_account_unverified', 'unexpected_cookie_scope', 'source_state_too_large', 'source_capture_failed'].includes(result.code)) sourceFailure = result.code;
  if (typeof result.stage === 'string' && ['source_url', 'source_reload', 'source_account', 'source_storage', 'source_cookies', 'source_encryption'].includes(result.stage)) sourceStage = result.stage;
  requireThat(result.ok === true && result.envelope, result.code === 'source_account_unverified' ? 'account_mismatch' : 'authentication_required');
  stage = 'validating_candidate';
  const staging = join(state, '.provision-' + randomUUID() + '.enc');
  try {
    const bytes = JSON.stringify(result.envelope); atomicPrivate(staging, bytes, true);
    const session = validateSession(encrypted.read(staging, SESSION_CONTEXT), config.customerId);
    requireThat(Number.isFinite(Date.parse(session.capturedAt)) && Math.abs(Date.now() - Date.parse(session.capturedAt)) < 5 * 60 * 1000, 'state_invalid');
    atomicPrivate(join(state, 'session-candidate.enc'), bytes);
    console.log(JSON.stringify({ outcome: 'candidate_captured', capturedAt: session.capturedAt,
      cookieCount: session.storageState.cookies.length, encryptedAtSource: true, activeSessionReplaced: false,
      next: 'Run run-isolated.py --mode verify-session with this private directory.' }));
  } finally { try { unlinkSync(staging); } catch {} }
} catch (error) {
  const failure = error instanceof Failure ? error : safeFailure(error, 'configuration_invalid');
  console.log(JSON.stringify({ outcome: 'failed', code: failure.code, stage, sourceFailure, sourceStage, activeSessionReplaced: false })); process.exitCode = 1;
} finally { await mcp?.close(); }
