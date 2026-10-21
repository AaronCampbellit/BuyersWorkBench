import test from 'node:test';
import assert from 'node:assert/strict';
import { generateKeyPairSync, randomUUID } from 'node:crypto';
import { mkdtempSync, writeFileSync, readFileSync, rmSync, chmodSync, symlinkSync, readdirSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { EncryptedFiles, SESSION_CONTEXT, OBSERVATION_CONTEXT } from '../src/crypto.ts';
import { SnapshotStore } from '../src/storage.ts';
import { readPrivate } from '../src/private-files.ts';
import { Failure } from '../src/errors.ts';
import type { Snapshot } from '../src/collector.ts';

const keys = generateKeyPairSync('rsa', { modulusLength: 3072 });
function setup() {
  const root = mkdtempSync(join(tmpdir(), 'bwb-lenovo-test-')), key = join(root, 'key.pem');
  writeFileSync(key, keys.privateKey.export({ type: 'pkcs8', format: 'pem' }), { mode: 0o600 });
  return { root, key, crypto: new EncryptedFiles(key) };
}
function snapshot(): Snapshot {
  return { schemaVersion: 'lenovo_orders.v1', connectorVersion: '0.1.0', origin: 'live_private', accountBinding: 'fictional-binding',
    runId: randomUUID(), startedAt: '2026-10-20T19:00:00Z', finishedAt: '2026-10-20T19:01:00Z',
    scope: { kind: 'placed_date', dateFrom: '2026-07-23', dateTo: '2026-10-20', days: 90, timezone: 'America/Chicago' },
    coverage: { complete: true, ordersListed: 1, ordersCollected: 1, listPages: 2, listPasses: 2 },
    orders: [{ orderId: 'FICT-ORDER', orderNumber: 'FICT-NUMBER', placedDate: '2026-10-01', sourceStatus: '2', lines: [], linkedTrackingInformation: [] }] };
}
test('encrypted documents authenticate their context, reject tampering and contain no plaintext', () => {
  const { root, crypto } = setup();
  try {
    const file = join(root, 'document.enc'), value = { private: 'INVENTED-SECRET-DO-NOT-LOG' };
    crypto.write(file, value, SESSION_CONTEXT);
    assert.deepEqual(crypto.read(file, SESSION_CONTEXT), value);
    assert.equal(readFileSync(file, 'utf8').includes(value.private), false);
    assert.throws(() => crypto.read(file, OBSERVATION_CONTEXT), Failure);
    const envelope = JSON.parse(readFileSync(file, 'utf8')), bytes = Buffer.from(envelope.ciphertext, 'base64');
    bytes[0] ^= 1; envelope.ciphertext = bytes.toString('base64'); writeFileSync(file, JSON.stringify(envelope));
    assert.throws(() => crypto.read(file, SESSION_CONTEXT), Failure);
  } finally { rmSync(root, { recursive: true, force: true }); }
});
test('unsafe permissions and symbolic secret files fail closed', () => {
  const { root, key } = setup();
  try {
    const symbolic = join(root, 'link.pem'); symlinkSync(key, symbolic);
    assert.throws(() => readPrivate(symbolic), Failure);
    chmodSync(key, 0o644); assert.throws(() => new EncryptedFiles(key), Failure);
  } finally { rmSync(root, { recursive: true, force: true }); }
});
test('repeat captures preserve history without duplicate source changes', () => {
  const { root, crypto } = setup();
  try {
    const store = new SnapshotStore(join(root, 'data'), crypto), first = snapshot();
    assert.equal(store.commit(first).changed, true);
    const next = { ...first, runId: randomUUID(), finishedAt: '2026-10-20T19:02:00Z' };
    const second = store.commit(next);
    assert.equal(second.changed, false); assert.equal(second.changeCount, 0);
    assert.equal(store.last()!.runId, next.runId);
    assert.equal(readdirSync(join(root, 'data', 'snapshots')).length, 2);
  } finally { rmSync(root, { recursive: true, force: true }); }
});
test('incomplete results, account changes and duplicate run writes preserve last success', () => {
  const { root, crypto } = setup();
  try {
    const store = new SnapshotStore(join(root, 'data'), crypto), first = snapshot(); store.commit(first);
    const pointer = join(root, 'data', 'last-success.json'), before = readFileSync(pointer, 'utf8');
    assert.throws(() => store.commit({ ...snapshot(), orders: [] }), Failure);
    assert.equal(readFileSync(pointer, 'utf8'), before);
    assert.throws(() => store.commit({ ...snapshot(), accountBinding: 'another-account' }), Failure);
    assert.equal(readFileSync(pointer, 'utf8'), before);
    assert.throws(() => store.commit(first), Failure);
    assert.equal(readFileSync(pointer, 'utf8'), before);
  } finally { rmSync(root, { recursive: true, force: true }); }
});
