// Link extraction does not contact the carrier. Only the observed UPS source
// format is reviewed; new carriers/formats remain explicit, non-clickable facts.
export interface TrackingLink {
  status: 'available' | 'not_provided' | 'unreviewed' | 'invalid' | 'number_mismatch';
  carrier: 'UPS' | null;
  url: string | null;
}
const legacyParameters = new Set(['HTMLVersion', 'loc', 'Requester', 'WBPM_lid', 'trackNums', 'track.x', 'track']);
export function trackingLink(value: unknown, trackingNumber: string | null, carrier: string | null): TrackingLink {
  const unavailable = (status: Exclude<TrackingLink['status'], 'available'>): TrackingLink => ({ status, carrier: null, url: null });
  if (value === undefined || value === null || value === '') return unavailable('not_provided');
  // Reject browser URL repairs, hidden controls and fragments before parsing.
  if (typeof value !== 'string' || value.length > 4096 || !/^https?:\/\//i.test(value) || /[\s\\\u0000-\u001f\u007f]|%(?:0[0-9a-f]|1[0-9a-f]|7f)/i.test(value)) return unavailable('invalid');
  let source: URL;
  try { source = new URL(value); } catch { return unavailable('invalid'); }
  if (!['https:', 'http:'].includes(source.protocol) || source.username || source.password || source.port || source.hash) return unavailable('invalid');
  if (source.hostname !== 'wwwapps.ups.com' || source.pathname !== '/WebTracking/track' || carrier?.toUpperCase() !== 'UPS') return unavailable('unreviewed');
  const keys = [...source.searchParams.keys()];
  if (keys.some(key => !legacyParameters.has(key)) || new Set(keys).size !== keys.length) return unavailable('unreviewed');
  const parameter = 'trackNums';
  if (!trackingNumber || !/^[A-Za-z0-9]{6,64}$/.test(trackingNumber) || source.searchParams.get(parameter) !== trackingNumber) return unavailable('number_mismatch');
  // Build from reviewed facts: do not carry marketing, account or session query
  // values into the record. Upgrade Lenovo's observed HTTP links to HTTPS.
  const safe = new URL('https://wwwapps.ups.com/WebTracking/track');
  safe.searchParams.set('track', 'yes');
  safe.searchParams.set(parameter, trackingNumber);
  return { status: 'available', carrier: 'UPS', url: safe.href };
}
