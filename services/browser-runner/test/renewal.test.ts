import test from 'node:test';
import assert from 'node:assert/strict';
import type { Session } from '../src/session.ts';
import type { Snapshot } from '../src/collector.ts';
import { sessionClocks, renewalDue, verifyRenewal } from '../src/renewal.ts';
import { collectWithRenewal } from '../src/session-flow.ts';
import { Failure } from '../src/errors.ts';

const now = new Date('2026-10-21T12:00:00Z');
function seed(passport = '2026-10-21T15:00:00Z', portal = '2026-10-21T15:30:00Z'): Session {
  const cookie = (name: string, domain: string, expires: string) => ({ name, domain, value: 'FICTIONAL-SECRET', expires: Date.parse(expires) / 1000,
    path: '/', httpOnly: true, secure: true, sameSite: 'None' as const });
  return { schema: 1, capturedAt: now.toISOString(), baseline: { customerId: 'FICT-CUSTOMER' }, locale: 'en-US', timezone: 'America/Chicago',
    sessionStorage: { origin: 'https://account.lenovo.com', values: {} }, storageState: { origins: [], cookies: [
      cookie('LPSWUST', '.passport.lenovo.com', passport), cookie('LPSWUTGT', '.passport.lenovo.com', passport),
      cookie('gateKeeper-token', '.lenovo.com', portal), cookie('online-token', '.lenovo.com', portal),
      cookie('WEBAUTHNTGC', '.bpsso.lenovo.com', '2026-10-30T12:00:00Z'), cookie('LPSWUST', '.bpsso.lenovo.com', '2026-10-30T12:00:00Z'),
      cookie('ttl-token', '.lenovo.com', '2027-02-21T12:00:00Z') ] } };
}
test('main sign-in limits renewal even while portal, business and long-retention cookies remain', () => {
  const clocks = sessionClocks(seed('2026-10-21T12:09:00Z'));
  assert.equal(clocks.limitingExpiryAt, '2026-10-21T12:09:00.000Z');
  assert.equal(renewalDue(clocks, now), true);
  assert.equal(renewalDue(sessionClocks(seed()), now), false);
  const unknown = seed(); unknown.storageState.cookies = unknown.storageState.cookies.filter(c => c.domain !== '.passport.lenovo.com');
  assert.equal(sessionClocks(unknown).complete, false);
});
test('portal-only refresh is not accepted as proof of renewed underlying sign-in', () => {
  const before = sessionClocks(seed());
  const portal = verifyRenewal(before, sessionClocks(seed(undefined, '2026-10-21T18:00:00Z')), true, 'explicit');
  assert.equal(portal.status, 'portal_only'); assert.equal(portal.passportExtended, false);
  const full = verifyRenewal(before, sessionClocks(seed('2026-10-21T18:00:00Z', '2026-10-21T18:00:00Z')), true, 'explicit');
  assert.equal(full.status, 'full'); assert.equal(full.authenticatedCollectionVerified, true);
  assert.equal(verifyRenewal(before, before, true, 'explicit').status, 'unchanged');
});
test('proactive renewal runs before collection even when initialization still authenticates', async () => {
  const calls: string[] = [], initial = seed('2026-10-21T12:09:00Z');
  const result = await collectWithRenewal({ initialize: async () => { calls.push('initialize'); }, renew: async () => { calls.push('renew'); },
    collect: async () => { calls.push('collect'); return {} as Snapshot; }, capture: async () => seed(), renewalObserved: () => true }, initial, false, now, new AbortController().signal);
  assert.deepEqual(calls, ['initialize', 'renew', 'initialize', 'collect']);
  assert.equal(result.renewal.reason, 'before_expiry');
});
test('explicit renewal runs while far from expiry; ordinary fresh collection does not force it', async () => {
  let renewals = 0;
  const client = { initialize: async () => {}, renew: async () => { renewals++; }, collect: async () => ({} as Snapshot), capture: async () => seed(), renewalObserved: () => false };
  const normal = await collectWithRenewal(client, seed(), false, now, new AbortController().signal);
  assert.equal(renewals, 0); assert.equal(normal.renewal.status, 'not_attempted');
  await collectWithRenewal(client, seed(), true, now, new AbortController().signal); assert.equal(renewals, 1);
});
test('failed renewal never captures replacement state or continues collection', async () => {
  let renewals = 0, collects = 0, captures = 0;
  await assert.rejects(collectWithRenewal({ initialize: async () => {}, renew: async () => { renewals++; throw new Failure('authentication_required'); },
    collect: async () => { collects++; return {} as Snapshot; }, capture: async () => { captures++; return seed(); }, renewalObserved: () => true },
  seed(), true, now, new AbortController().signal), (e: unknown) => e instanceof Failure && e.code === 'authentication_required');
  assert.equal(renewals, 2); assert.equal(collects, 0); assert.equal(captures, 0);
});
