import test from 'node:test';
import assert from 'node:assert/strict';
import { generateKeyPairSync, constants, privateDecrypt, createDecipheriv } from 'node:crypto';
import { createServer } from 'node:http';
import type { ServerResponse } from 'node:http';
import { mkdtempSync, mkdirSync, writeFileSync, readFileSync, rmSync, statSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { fileURLToPath } from 'node:url';
import { execFile } from 'node:child_process';
import { promisify } from 'node:util';
import type { Page } from 'playwright';
import { captureScopedSession } from '../src/source-export.ts';
import { localMcpUrl, LocalMcp } from '../src/mcp-client.ts';

const keys = generateKeyPairSync('rsa', { modulusLength: 3072 });
const publicKey = keys.publicKey.export({ type: 'spki', format: 'der' }).toString('base64');
function source(customer = 'FICT-CUSTOMER', url = 'https://account.lenovo.com/us/lenovopro/en/account/order/list.html') {
  return { url: () => url, waitForResponse: async () => ({ status: () => 200,
    json: async () => ({ status: 200, data: { page: { datas: [{ customerId: customer }] } } }) }), reload: async () => {},
    evaluate: async () => ({ origin: 'https://account.lenovo.com', localStorage: [{ name: 'provider', value: 'FICTIONAL-STORAGE-SECRET' }],
      sessionStorage: {}, locale: 'en-US', timezone: 'America/Chicago' }),
    context: () => ({ cookies: async (urls: string[]) => {
      assert.ok(urls.every(url => new URL(url).hostname.endsWith('.lenovo.com')));
      assert.ok(urls.includes(url), 'capture cookies applicable to the selected order page');
      assert.ok(urls.includes('https://openapi.lenovo.com/us/lenovopro/en/ofp/order/authDetail'));
      assert.ok(urls.includes('https://openapi.lenovo.com/us/lenovopro/en/ofp/auth/mobileOrder/list'));
      return [{ name: 'FICT-COOKIE', value: 'FICTIONAL-COOKIE-SECRET', domain: '.lenovo.com', path: '/', expires: -1, httpOnly: true, secure: true, sameSite: 'Lax' }];
    } }) } as unknown as Page;
}
test('source-side capture transfers ciphertext only and decrypts under the session context', async () => {
  const result = await captureScopedSession(source(), 'FICT-CUSTOMER', publicKey);
  assert.equal(result.ok, true); assert.ok(result.envelope);
  assert.equal(JSON.stringify(result).includes('FICTIONAL-COOKIE-SECRET'), false);
  assert.equal(JSON.stringify(result).includes('FICTIONAL-STORAGE-SECRET'), false);
  const envelope = result.envelope!;
  const key = privateDecrypt({ key: keys.privateKey, padding: constants.RSA_PKCS1_OAEP_PADDING, oaepHash: 'sha256' }, Buffer.from(envelope.wrappedKey, 'base64'));
  const bytes = Buffer.from(envelope.ciphertext, 'base64');
  const cipher = createDecipheriv('aes-256-gcm', key, Buffer.from(envelope.iv, 'base64'));
  cipher.setAAD(Buffer.from(envelope.context)); cipher.setAuthTag(bytes.subarray(-16));
  const decoded = JSON.parse(Buffer.concat([cipher.update(bytes.subarray(0, -16)), cipher.final()]).toString('utf8')); key.fill(0);
  assert.equal(decoded.baseline.customerId, 'FICT-CUSTOMER'); assert.equal(decoded.storageState.cookies[0].value, 'FICTIONAL-COOKIE-SECRET');
  const serialized = new Function('return (' + captureScopedSession.toString() + ')')() as typeof captureScopedSession;
  assert.equal((await serialized(source('ANOTHER-CUSTOMER'), 'FICT-CUSTOMER', publicKey)).code, 'source_account_unverified');
});
test('wrong account or tab cannot produce a capsule', async () => {
  assert.equal((await captureScopedSession(source('OTHER'), 'FICT-CUSTOMER', publicKey)).ok, false);
  assert.equal((await captureScopedSession(source('FICT-CUSTOMER', 'https://unrelated.invalid/'), 'FICT-CUSTOMER', publicKey)).code, 'select_authenticated_lenovo_tab');
  const broken = source();
  broken.evaluate = async () => { throw new Error('FICTIONAL-PRIVATE-PROVIDER-EXCEPTION'); };
  const failed = await captureScopedSession(broken, 'FICT-CUSTOMER', publicKey);
  assert.deepEqual(failed, { ok: false, code: 'source_capture_failed', stage: 'source_storage' });
  assert.equal(JSON.stringify(failed).includes('FICTIONAL-PRIVATE-PROVIDER-EXCEPTION'), false);
});
test('MCP transport answers server heartbeat requests over GET SSE and preserves loopback/Host boundaries', async () => {
  for (const url of ['https://127.0.0.1/mcp', 'http://169.254.169.254/mcp', 'http://user:secret@127.0.0.1/mcp', 'http://127.0.0.1/mcp?token=x']) assert.throws(() => localMcpUrl(url));
  const seen: string[] = [];
  let events: ServerResponse | undefined, pending: ServerResponse | undefined, pendingId: number | undefined;
  let eventReady!: () => void;
  const connected = new Promise<void>(resolve => { eventReady = resolve; });
  const toolResult = { content: [{ type: 'text', text: '### Result\n' + JSON.stringify({ ok: true, envelope: 'FICT-CIPHERTEXT' }) + '\n### Ran Playwright code\nREDACTED' }] };
  let failTool = false;
  const server = createServer(async (req, res) => {
    assert.ok(req.headers.host?.startsWith('localhost:'), 'preserve the MCP server accepted localhost Host header');
    if (req.method === 'GET') {
      res.writeHead(200, { 'Content-Type': 'text/event-stream' }); res.flushHeaders(); events = res; eventReady(); return;
    }
    let raw = ''; for await (const part of req) raw += part;
    const message = JSON.parse(raw);
    if (!message.method) {
      seen.push('ping_reply'); assert.equal(message.id, pendingId); assert.deepEqual(message.result, {});
      res.writeHead(202); res.end();
      const result = failTool ? { isError: true, content: [{ type: 'text', text: 'FICTIONAL-PRIVATE-BROWSER-EXCEPTION' }] } : toolResult;
      pending!.end('event: message\ndata: ' + JSON.stringify({ jsonrpc: '2.0', id: pendingId, result }) + '\n\n'); return;
    }
    seen.push(message.method);
    if (message.method !== 'initialize') assert.equal(req.headers['mcp-session-id'], 'FICT-SESSION');
    if (message.id === undefined) { res.writeHead(202); res.end(); return; }
    if (message.method === 'tools/call') {
      await connected; pending = res; pendingId = message.id;
      res.writeHead(200, { 'Content-Type': 'text/event-stream' }); res.flushHeaders();
      // Same ID in the opposite direction must remain a server request,
      // never be mistaken for the tool result or silently ignored.
      events!.write('event: message\ndata: ' + JSON.stringify({ jsonrpc: '2.0', id: message.id, method: 'ping' }) + '\n\n'); return;
    }
    const result = message.method === 'initialize' ? { protocolVersion: '2025-03-26', capabilities: { tools: {} }, serverInfo: { name: 'FICT-SERVER', version: '1' } }
      : { tools: [{ name: 'browser_run_code_unsafe', inputSchema: { type: 'object' } }] };
    res.writeHead(200, { 'Content-Type': 'text/event-stream', 'Mcp-Session-Id': 'FICT-SESSION' });
    res.end('event: message\ndata: ' + JSON.stringify({ jsonrpc: '2.0', id: message.id, result }) + '\n\n');
  });
  await new Promise<void>(resolve => server.listen(0, '127.0.0.1', resolve));
  let mcp: LocalMcp | undefined;
  try {
    const address = server.address(); assert.ok(address && typeof address === 'object');
    mcp = new LocalMcp('http://localhost:' + address.port + '/mcp');
    const tool = await mcp.initialize();
    assert.deepEqual(await mcp.run(tool, 'async () => ({ok:true})'), { ok: true, envelope: 'FICT-CIPHERTEXT' });
    failTool = true;
    await assert.rejects(mcp.run(tool, 'async () => ({ok:true})'), (error: unknown) => {
      const failure = error as { code: string; message: string };
      return failure.code === 'network_error' && !failure.message.includes('FICTIONAL-PRIVATE-BROWSER-EXCEPTION');
    });
    assert.deepEqual(seen, ['initialize', 'notifications/initialized', 'tools/list', 'tools/call', 'ping_reply', 'tools/call', 'ping_reply']);
  } finally { await mcp?.close(); server.closeAllConnections(); await new Promise<void>(resolve => server.close(() => resolve())); }
});
test('provisioning stages a valid candidate and preserves active credentials/history on account mismatch', { skip: process.getuid?.() === 0 }, async () => {
  const root = mkdtempSync(join(tmpdir(), 'bwb-provision-test-'));
  for (const name of ['credentials', 'state', 'data']) mkdirSync(join(root, name), { mode: 0o700 });
  writeFileSync(join(root, 'credentials/key.pem'), keys.privateKey.export({ type: 'pkcs8', format: 'pem' }), { mode: 0o600 });
  writeFileSync(join(root, 'credentials/config.json'), JSON.stringify({ schema: 1, customerId: 'FICT-CUSTOMER', timezone: 'America/Chicago' }), { mode: 0o600 });
  writeFileSync(join(root, 'state/session.enc'), 'EXISTING-ACTIVE-SESSION', { mode: 0o600 });
  writeFileSync(join(root, 'data/last-success.json'), 'EXISTING-ACCEPTED-HISTORY', { mode: 0o600 });
  let result = await captureScopedSession(source(), 'FICT-CUSTOMER', publicKey);
  const server = createServer(async (req, res) => {
    if (req.method === 'GET') { res.writeHead(405); res.end(); return; }
    let text = ''; for await (const part of req) text += part;
    const request = JSON.parse(text);
    if (request.id === undefined) { res.writeHead(202); res.end(); return; }
    const value = request.method === 'initialize' ? { protocolVersion: '2025-03-26', capabilities: { tools: {} }, serverInfo: { name: 'FICT-SERVER', version: '1' } } : request.method === 'tools/list'
      ? { tools: [{ name: 'browser_run_code_unsafe', inputSchema: { type: 'object' } }] } : { content: [{ type: 'text', text: '### Result\n' + JSON.stringify(result) }] };
    res.writeHead(200, { 'Content-Type': 'application/json' }); res.end(JSON.stringify({ jsonrpc: '2.0', id: request.id, result: value }));
  });
  await new Promise<void>(resolve => server.listen(0, '127.0.0.1', resolve));
  try {
    const address = server.address(); assert.ok(address && typeof address === 'object');
    const args = [fileURLToPath(new URL('../provision-session.py', import.meta.url)), '--private-dir', root, '--mcp-url', 'http://127.0.0.1:' + address.port + '/mcp'];
    const run = promisify(execFile);
    const success = await run('python3', args);
    assert.equal(JSON.parse(success.stdout).outcome, 'candidate_captured');
    assert.equal(success.stdout.includes('FICTIONAL-COOKIE-SECRET'), false);
    const candidate = readFileSync(join(root, 'state/session-candidate.enc'));
    assert.equal(statSync(join(root, 'state/session-candidate.enc')).mode & 0o777, 0o600);
    result = await captureScopedSession(source('OTHER-CUSTOMER'), 'OTHER-CUSTOMER', publicKey);
    await assert.rejects(run('python3', args), (error: unknown) => JSON.parse((error as { stdout: string }).stdout).code === 'account_mismatch');
    assert.equal(readFileSync(join(root, 'state/session.enc'), 'utf8'), 'EXISTING-ACTIVE-SESSION');
    assert.equal(readFileSync(join(root, 'data/last-success.json'), 'utf8'), 'EXISTING-ACCEPTED-HISTORY');
    assert.deepEqual(readFileSync(join(root, 'state/session-candidate.enc')), candidate);
  } finally { server.closeAllConnections(); await new Promise<void>(resolve => server.close(() => resolve())); rmSync(root, { recursive: true, force: true }); }
});
