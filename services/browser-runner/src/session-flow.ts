import type { Session } from './session.ts';
import type { Snapshot } from './collector.ts';
import { withAuthenticationRecovery } from './collector.ts';
import { sessionClocks, renewalDue, verifyRenewal } from './renewal.ts';
import { Failure } from './errors.ts';

export interface SessionFlow {
  initialize(): Promise<void>; renew(): Promise<void>; collect(): Promise<Snapshot>;
  capture(): Promise<Session>; renewalObserved(): boolean;
}
// A renewed HTTP response is insufficient: complete authenticated collection
// and captured provider/portal clocks are required before any state is saved.
export async function collectWithRenewal(client: SessionFlow, seed: Session, explicit: boolean, now: Date, signal: AbortSignal) {
  if (signal.aborted) throw new Failure('collection_cancelled');
  const before = sessionClocks(seed), due = renewalDue(before, now);
  let proactiveCalled = false;
  const snapshot = await withAuthenticationRecovery(async () => {
    await client.initialize();
    if ((explicit || due) && !proactiveCalled) {
      proactiveCalled = true;
      await client.renew();
      await client.initialize();
    }
    return client.collect();
  }, () => client.renew(), signal);
  if (signal.aborted) throw new Failure('collection_cancelled');
  const session = await client.capture(), observed = client.renewalObserved();
  if (signal.aborted) throw new Failure('collection_cancelled');
  return { snapshot, session, renewal: verifyRenewal(before, sessionClocks(session), proactiveCalled || observed,
    explicit ? 'explicit' : proactiveCalled ? 'before_expiry' : observed ? 'recovery_or_website' : 'not_due') };
}
