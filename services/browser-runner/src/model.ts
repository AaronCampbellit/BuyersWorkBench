import { createHash } from 'node:crypto';
import { requireThat } from './errors.ts';
import { trackingLink } from './tracking.ts';
import type { TrackingLink } from './tracking.ts';

export type ObjectValue = Record<string, unknown>;
export function object(value: unknown): ObjectValue {
  requireThat(value !== null && typeof value === 'object' && !Array.isArray(value));
  return value as ObjectValue;
}
export function text(value: unknown, optional = false, maximum = 240): string | null {
  if (optional && (value === undefined || value === null || value === '')) return null;
  requireThat(typeof value === 'string' && value.trim().length > 0 && value.length <= maximum && !/[\u0000-\u001f\u007f]/.test(value));
  return value.trim();
}
export function identifier(value: unknown): string {
  if (typeof value === 'number') requireThat(Number.isSafeInteger(value) && value >= 0);
  else requireThat(typeof value === 'string');
  return text(String(value), false, 120)!;
}
export function integer(value: unknown, maximum = 100_000): number {
  requireThat(typeof value === 'number' && Number.isSafeInteger(value) && value >= 0 && value <= maximum);
  return value;
}
export function array(value: unknown, maximum = 1000): unknown[] {
  requireThat(Array.isArray(value) && value.length <= maximum);
  return value;
}
export function optionalBoolean(value: unknown): boolean | null {
  if (value === undefined || value === null) return null;
  requireThat(typeof value === 'boolean' || value === 0 || value === 1);
  return value === true || value === 1;
}
export function quantity(value: unknown, optional = false): string | null {
  if (optional && (value === undefined || value === null || value === '')) return null;
  // JSON fractional numbers cannot preserve exact decimal input; reject them.
  if (typeof value === 'number') requireThat(Number.isSafeInteger(value) && value >= 0);
  else requireThat(typeof value === 'string');
  const raw = String(value);
  requireThat(/^(0|[1-9][0-9]{0,11})(\.[0-9]{1,9})?$/.test(raw));
  return raw.includes('.') ? raw.replace(/0+$/, '').replace(/\.$/, '') : raw;
}
function scaled(value: string): bigint {
  const [whole, fraction = ''] = value.split('.');
  return BigInt(whole) * 1_000_000_000n + BigInt(fraction.padEnd(9, '0'));
}

// Date-only facts preserve the supplier's displayed calendar date. No fuzzy
// Date.parse, UTC conversion or inference from a status/timeline label.
export function sourceDate(value: unknown): string | null {
  if (value === undefined || value === null || value === '') return null;
  const raw = text(value)!;
  let match = raw.match(/^(\d{4})-(\d{2})-(\d{2})(?: (\d{2}):(\d{2}):(\d{2}))?$/);
  let result: string;
  if (match) {
    if (match[4]) requireThat(Number(match[4]) <= 23 && Number(match[5]) <= 59 && Number(match[6]) <= 59);
    result = `${match[1]}-${match[2]}-${match[3]}`;
  } else {
    match = raw.match(/^(\d{2})\/(\d{2})\/(\d{4})$/);
    requireThat(match); result = `${match[3]}-${match[1]}-${match[2]}`;
  }
  const date = new Date(`${result}T00:00:00Z`);
  requireThat(Number.isFinite(date.getTime()) && date.toISOString().slice(0, 10) === result && result >= '2000-01-01' && result <= '2100-12-31');
  return result;
}
export interface Scope { kind: 'placed_date'; dateFrom: string; dateTo: string; days: 90; timezone: string }
export function rollingScope(now: Date, timezone: string): Scope {
  const parts = new Intl.DateTimeFormat('en-US', { timeZone: timezone, year: 'numeric', month: '2-digit', day: '2-digit' }).formatToParts(now);
  const part = (type: string) => parts.find(p => p.type === type)!.value;
  const dateTo = `${part('year')}-${part('month')}-${part('day')}`;
  const start = new Date(`${dateTo}T00:00:00Z`); start.setUTCDate(start.getUTCDate() - 89);
  return { kind: 'placed_date', dateFrom: start.toISOString().slice(0, 10), dateTo, days: 90, timezone };
}
export function digest(value: unknown): string {
  return createHash('sha256').update(JSON.stringify(value)).digest('hex');
}

export interface OrderRef { orderId: string; orderNumber: string; customerId: string; placedDate: string; sourceStatus: string }
export interface ListPage { number: number; size: number; total: number; pages: number; version: number; orders: OrderRef[] }
export function envelope(value: unknown): ObjectValue {
  const root = object(value);
  requireThat(root.status === 200);
  return object(root.data);
}
export function parseList(value: unknown, expectedCustomer: string): ListPage {
  const page = object(envelope(value).page);
  const orders = array(page.datas).map(value => {
    const row = object(value);
    const customerId = identifier(row.customerId);
    requireThat(customerId === expectedCustomer, 'account_mismatch');
    return { orderId: identifier(row.eCommerceId), orderNumber: identifier(row.orderNumber), customerId,
      placedDate: sourceDate(row.placeOrderDate)!, sourceStatus: identifier(row.orderStatus) };
  });
  requireThat(orders.every(r => r.placedDate));
  const result = { number: integer(page.pageNum), size: integer(page.pageSize, 100), total: integer(page.totalCount),
    pages: integer(page.totalPageNum, 10000), version: integer(page.version, Number.MAX_SAFE_INTEGER), orders };
  requireThat(result.number > 0 && result.size > 0 && orders.length <= result.size);
  requireThat(result.total === 0 ? (result.pages === 0 || result.pages === 1) : result.pages === Math.ceil(result.total / result.size), 'incomplete_scope');
  return result;
}

export interface Shipment {
  key: string | null; sourceDeliveryNumber: string | null; carrier: string | null; trackingNumber: string | null;
  trackingLink: TrackingLink;
  quantity: string | null; estimatedArrivalDate: string | null; estimatedArrivalLabel: string | null;
  arrivalYearMissing: boolean; deliveredDate: string | null; delayed: boolean | null;
}
function shipmentArrival(value: unknown): Pick<Shipment, 'estimatedArrivalDate' | 'estimatedArrivalLabel' | 'arrivalYearMissing'> {
  const label = text(value, true);
  if (label && /^Get it by (Mon|Tue|Wed|Thu|Fri|Sat|Sun)\. (Jan|Feb|Mar|Apr|May|Jun|Jul|Aug|Sep|Oct|Nov|Dec) ([1-9]|[12][0-9]|3[01])$/.test(label))
    return { estimatedArrivalDate: null, estimatedArrivalLabel: label, arrivalYearMissing: true };
  return { estimatedArrivalDate: sourceDate(value), estimatedArrivalLabel: label, arrivalYearMissing: false };
}
export interface Line {
  key: string; parentKey: string | null; relation: 'item' | 'component' | 'split'; sourceItemId: string; itemNo: string; lineNo: string;
  partNumber: string; description: string; quantity: string; shippedQuantity: string | null; cancelledQuantity: string | null;
  status: { code: string | null; label: string | null }; physicalProduct: boolean | null; digitalProduct: boolean | null;
  estimatedDeliveryDate: string | null; estimatedShipDate: string | null; carrierReportedDate: string | null;
  shipments: Shipment[] | null;
}
export interface LinkedTrackingInformation {
  label: 'UPS tracking'; carrier: 'UPS'; url: string; trackingNumber: string;
  lineKey: string; shipmentKey: string; source: 'lenovo_order_detail';
}
export interface Order {
  orderId: string; orderNumber: string; placedDate: string; sourceStatus: string; lines: Line[];
  linkedTrackingInformation: LinkedTrackingInformation[];
}
export function parseOrder(value: unknown, ref: OrderRef): Order {
  const data = envelope(value);
  requireThat(identifier(data.customerId) === ref.customerId, 'account_mismatch');
  requireThat(identifier(data.eCommerceId) === ref.orderId && identifier(data.orderNumber) === ref.orderNumber, 'account_mismatch');
  const placedDate = sourceDate(data.placeOrderDate);
  requireThat(placedDate === ref.placedDate, 'incomplete_scope');
  const lines: Line[] = [], seen = new Set<string>();
  function visit(value: unknown, parentKey: string | null, relation: Line['relation'], depth: number): void {
    requireThat(depth <= 5 && lines.length < 1000);
    const item = object(value);
    requireThat(identifier(item.orderNumber) === ref.orderNumber, 'account_mismatch');
    const sourceItemId = identifier(item.itemId), itemNo = identifier(item.itemNo), lineNo = identifier(item.lineNo);
    const key = digest([ref.orderId, parentKey, relation, sourceItemId]);
    requireThat(!seen.has(key)); seen.add(key);
    const ordered = quantity(item.quantity)!, shipped = quantity(item.shippedQuantity, true), cancelled = quantity(item.cancelledQuantity, true);
    requireThat(scaled(ordered) > 0n);
    if (shipped !== null) requireThat(scaled(shipped) <= scaled(ordered));
    if (cancelled !== null) requireThat(scaled(cancelled) <= scaled(ordered));
    if (shipped !== null && cancelled !== null) requireThat(scaled(shipped) + scaled(cancelled) <= scaled(ordered));
    const shipmentKeys = new Set<string>();
    const shipments = item.shippingStatusVOList == null ? null : array(item.shippingStatusVOList).map(value => {
      const shipment = object(value);
      const carrier = text(shipment.carrier, true), trackingNumber = text(shipment.trackingNumber, true), sourceDeliveryNumber = text(shipment.ovpDeliveryNumber, true);
      const identity = trackingNumber ? ['tracking', carrier, trackingNumber] : sourceDeliveryNumber ? ['delivery', sourceDeliveryNumber] : null;
      const shipmentKey = identity ? digest([key, ...identity]) : null;
      if (shipmentKey) { requireThat(!shipmentKeys.has(shipmentKey)); shipmentKeys.add(shipmentKey); }
      return { key: shipmentKey, sourceDeliveryNumber, carrier, trackingNumber, trackingLink: trackingLink(shipment.trackingUrl, trackingNumber, carrier), quantity: quantity(shipment.quantity, true),
        ...shipmentArrival(shipment.estimatedArrivalDate), deliveredDate: sourceDate(shipment.deliveredDate), delayed: optionalBoolean(shipment.delay) };
    }).sort((a, b) => (a.key ?? digest(a)).localeCompare(b.key ?? digest(b)));
    lines.push({ key, parentKey, relation, sourceItemId, itemNo, lineNo,
      partNumber: text(item.productNumber)!, description: text(item.productDesc, false, 2000)!, quantity: ordered, shippedQuantity: shipped,
      cancelledQuantity: cancelled, status: { code: item.orderStatus == null || item.orderStatus === '' ? null : identifier(item.orderStatus), label: text(item.orderStatusName, true) },
      physicalProduct: optionalBoolean(item.physicalProduct), digitalProduct: optionalBoolean(item.isDigitalProduct),
      estimatedDeliveryDate: sourceDate(item.estimatedDeliveryDate), estimatedShipDate: sourceDate(item.estimatedShipDate),
      carrierReportedDate: sourceDate(item.upsDeliveryDate), shipments });
    for (const child of item.componentItems == null ? [] : array(item.componentItems)) visit(child, key, 'component', depth + 1);
    for (const child of item.splitItems == null ? [] : array(item.splitItems)) visit(child, key, 'split', depth + 1);
  }
  const items = array(data.orderItemsVO); requireThat(items.length > 0);
  for (const item of items) visit(item, null, 'item', 0);
  lines.sort((a, b) => a.key.localeCompare(b.key));
  const linkedTrackingInformation: LinkedTrackingInformation[] = [];
  for (const line of lines) for (const shipment of line.shipments ?? []) {
    if (shipment.trackingLink.status === 'available') {
      requireThat(shipment.key && shipment.trackingNumber && shipment.trackingLink.url);
      linkedTrackingInformation.push({ label: 'UPS tracking', carrier: 'UPS', url: shipment.trackingLink.url,
        trackingNumber: shipment.trackingNumber, lineKey: line.key, shipmentKey: shipment.key, source: 'lenovo_order_detail' });
    }
  }
  return { orderId: ref.orderId, orderNumber: ref.orderNumber, placedDate: placedDate!, sourceStatus: identifier(data.orderStatus),
    lines, linkedTrackingInformation };
}
