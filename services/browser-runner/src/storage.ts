import { existsSync } from 'node:fs';
import { join } from 'node:path';
import { EncryptedFiles, OBSERVATION_CONTEXT } from './crypto.ts';
import { atomicPrivate, privateDirectory, readPrivate } from './private-files.ts';
import { compare, snapshotDigest } from './collector.ts';
import type { Change, Snapshot } from './collector.ts';
import { requireThat } from './errors.ts';
import { object } from './model.ts';

export interface Commit { runId: string; changed: boolean; changeCount: number; orderCount: number; lineCount: number; shipmentCount: number; trackingLinkCount: number }
interface Observation { snapshot: Snapshot; changes: Change[]; previousRunId: string | null }
export class SnapshotStore {
  private readonly root: string;
  private readonly encrypted: EncryptedFiles;
  constructor(root: string, encrypted: EncryptedFiles) {
    privateDirectory(root); privateDirectory(join(root, 'snapshots')); privateDirectory(join(root, 'runs'));
    this.root = root; this.encrypted = encrypted;
  }
  last(): Snapshot | null {
    const pointer = join(this.root, 'last-success.json');
    if (!existsSync(pointer)) return null;
    const metadata = object(JSON.parse(readPrivate(pointer, 16384).toString('utf8')));
    requireThat(typeof metadata.runId === 'string' && /^[0-9a-f-]{36}$/.test(metadata.runId), 'storage_error');
    const previous = this.encrypted.read(join(this.root, 'snapshots', `${metadata.runId}.enc`), OBSERVATION_CONTEXT) as Observation;
    requireThat(previous.snapshot.schemaVersion === 'lenovo_orders.v1' && previous.snapshot.runId === metadata.runId && snapshotDigest(previous.snapshot) === metadata.digest, 'storage_error');
    return previous.snapshot;
  }
  commit(snapshot: Snapshot): Commit {
    const previous = this.last(), changes = compare(previous, snapshot), digest = snapshotDigest(snapshot);
    const result = { runId: snapshot.runId, changed: previous === null || snapshotDigest(previous) !== digest, changeCount: changes.length,
      orderCount: snapshot.orders.length, lineCount: snapshot.orders.reduce((n, o) => n + o.lines.length, 0),
      shipmentCount: snapshot.orders.reduce((n, o) => n + o.lines.reduce((m, l) => m + (l.shipments?.length ?? 0), 0), 0),
      trackingLinkCount: snapshot.orders.reduce((n, o) => n + o.linkedTrackingInformation.length, 0) };
    this.encrypted.write(join(this.root, 'snapshots', `${snapshot.runId}.enc`),
      { snapshot, changes, previousRunId: previous?.runId ?? null } satisfies Observation, OBSERVATION_CONTEXT, true);
    this.attempt({ outcome: 'collected', ...result }, snapshot.runId);
    // This single atomic pointer is the acceptance point. Failed parsing,
    // pagination, identity checks or unexplained drops never reach it.
    atomicPrivate(join(this.root, 'last-success.json'), JSON.stringify({ ...result, digest, scope: snapshot.scope, finishedAt: snapshot.finishedAt }));
    return result;
  }
  attempt(summary: Record<string, unknown>, runId: string): void {
    requireThat(/^[0-9a-f-]{36}$/.test(runId), 'storage_error');
    const report = JSON.stringify({ at: new Date().toISOString(), ...summary });
    atomicPrivate(join(this.root, 'runs', `${runId}.json`), report, true);
    atomicPrivate(join(this.root, 'last-attempt.json'), report);
  }
}
