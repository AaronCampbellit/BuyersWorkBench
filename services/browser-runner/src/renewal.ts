import type { Session } from './session.ts';

export interface SessionClock { expiresAt: string | null; complete: boolean }
export interface SessionClocks {
  passport: SessionClock; portal: SessionClock; businessSso: SessionClock;
  limitingExpiryAt: string | null; complete: boolean;
}
const passportNames = ['LPSWUST', 'LPSWUTGT'];
const portalNames = ['gateKeeper-token', 'online-token'];
export const RENEWAL_LEAD_SECONDS = 600;
export function sessionClocks(session: Session): SessionClocks {
  function clock(names: string[], domain: (value: string) => boolean): SessionClock {
    const matches = session.storageState.cookies.filter(c => names.includes(c.name) && domain(c.domain.replace(/^\./, '')) && Number.isFinite(c.expires) && c.expires > 0);
    return { expiresAt: matches.length ? new Date(Math.min(...matches.map(c => c.expires)) * 1000).toISOString() : null,
      complete: names.every(name => matches.some(c => c.name === name)) };
  }
  const passport = clock(passportNames, d => d === 'passport.lenovo.com');
  const portal = clock(portalNames, d => d === 'lenovo.com' || d === 'account.lenovo.com');
  const businessSso = clock(['WEBAUTHNTGC', 'LPSWUST'], d => d === 'bpsso.lenovo.com');
  const times = [passport.expiresAt, portal.expiresAt].filter((v): v is string => v !== null);
  return { passport, portal, businessSso, limitingExpiryAt: times.length ? times.sort()[0] : null,
    complete: passport.complete && portal.complete };
}
export function renewalDue(clocks: SessionClocks, now: Date): boolean {
  return clocks.limitingExpiryAt !== null && Date.parse(clocks.limitingExpiryAt) - now.getTime() <= RENEWAL_LEAD_SECONDS * 1000;
}
export interface RenewalVerification {
  attempted: boolean; reason: 'explicit' | 'before_expiry' | 'recovery_or_website' | 'not_due';
  status: 'full' | 'portal_only' | 'passport_only' | 'unchanged' | 'unknown' | 'not_attempted';
  passportExtended: boolean; portalExtended: boolean; authenticatedCollectionVerified: true;
  before: SessionClocks; after: SessionClocks;
}
export function verifyRenewal(before: SessionClocks, after: SessionClocks, attempted: boolean, reason: RenewalVerification['reason']): RenewalVerification {
  const extended = (a: SessionClock, b: SessionClock) => a.complete && b.complete && a.expiresAt !== null && b.expiresAt !== null && Date.parse(b.expiresAt) > Date.parse(a.expiresAt) + 1000;
  const passportExtended = extended(before.passport, after.passport), portalExtended = extended(before.portal, after.portal);
  const status = !attempted ? 'not_attempted' : !before.complete || !after.complete ? 'unknown'
    : passportExtended && portalExtended ? 'full' : passportExtended ? 'passport_only' : portalExtended ? 'portal_only' : 'unchanged';
  return { attempted, reason, status, passportExtended, portalExtended, authenticatedCollectionVerified: true, before, after };
}
