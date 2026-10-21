import { randomUUID } from 'node:crypto';
import { setTimeout as sleep } from 'node:timers/promises';
import { Failure, requireThat, retryable, safeFailure } from './errors.ts';
import { digest, parseList, parseOrder, rollingScope } from './model.ts';
import type { Order, OrderRef, Scope } from './model.ts';

export interface Transport {
  list(page: number): Promise<unknown>;
  detail(order: OrderRef): Promise<unknown>;
}
export interface Snapshot {
  schemaVersion: 'lenovo_orders.v1'; connectorVersion: '0.1.0' | '0.2.0'; origin: 'live_private';
  accountBinding: string; runId: string; startedAt: string; finishedAt: string;
  scope: Scope; coverage: { complete: true; ordersListed: number; ordersCollected: number; listPages: number; listPasses: 2 };
  orders: Order[];
}
export interface Options {
  customerId: string; timezone: string; now: Date; signal: AbortSignal;
  maxPages: number; maxOrders: number;
  sleep?: (ms: number) => Promise<void>;
}
export async function withAuthenticationRecovery<T>(operation: () => Promise<T>, renew: () => Promise<void>, signal: AbortSignal): Promise<T> {
  try { return await operation(); }
  catch (error) {
    if (!(error instanceof Failure) || error.code !== 'authentication_required') throw error;
    if (signal.aborted) throw new Failure('collection_cancelled');
    await renew();
    if (signal.aborted) throw new Failure('collection_cancelled');
    return operation(); // Exactly one fresh attempt, never a retry loop.
  }
}
async function read<T>(operation: () => Promise<T>, options: Options): Promise<T> {
  for (let attempt = 0; ; attempt++) {
    if (options.signal.aborted) throw new Failure('collection_cancelled');
    try { return await operation(); }
    catch (error) {
      const failure = safeFailure(error, 'network_error');
      if (!retryable(failure) || attempt >= 2 || failure.retryAfterMs > 10_000) throw failure;
      const delay = Math.max(failure.retryAfterMs, 500 * 2 ** attempt);
      try { await (options.sleep ? options.sleep(delay) : sleep(delay, undefined, { signal: options.signal })); }
      catch { throw new Failure('collection_cancelled'); }
    }
  }
}
async function listAll(transport: Transport, options: Options): Promise<{ orders: OrderRef[]; pages: number }> {
  const first = parseList(await read(() => transport.list(1), options), options.customerId);
  requireThat(first.number === 1, 'incomplete_scope');
  requireThat(first.pages <= options.maxPages && first.total <= options.maxOrders, 'incomplete_scope');
  const orders: OrderRef[] = [], seen = new Set<string>();
  function append(rows: OrderRef[]): void {
    for (const order of rows) {
      requireThat(!seen.has(order.orderId), 'incomplete_scope');
      seen.add(order.orderId); orders.push(order);
    }
  }
  append(first.orders);
  for (let number = 2; number <= first.pages; number++) {
    const page = parseList(await read(() => transport.list(number), options), options.customerId);
    requireThat(page.number === number && page.total === first.total && page.pages === first.pages && page.size === first.size && page.version === first.version, 'incomplete_scope');
    append(page.orders);
  }
  requireThat(orders.length === first.total, 'incomplete_scope');
  return { orders: orders.sort((a, b) => a.orderId.localeCompare(b.orderId)), pages: Math.max(1, first.pages) };
}

// A complete result is returned only after every requested detail and a second
// full list pass succeed. The caller persists it only after this function returns.
export async function collect(transport: Transport, options: Options): Promise<Snapshot> {
  const scope = rollingScope(options.now, options.timezone);
  const initial = await listAll(transport, options);
  const requested = initial.orders.filter(r => r.placedDate >= scope.dateFrom && r.placedDate <= scope.dateTo);
  const orders: Order[] = [];
  for (const ref of requested) orders.push(parseOrder(await read(() => transport.detail(ref), options), ref));
  const final = await listAll(transport, options);
  requireThat(digest(initial.orders) === digest(final.orders), 'incomplete_scope');
  requireThat(!options.signal.aborted, 'collection_cancelled');
  return { schemaVersion: 'lenovo_orders.v1', connectorVersion: '0.2.0', origin: 'live_private',
    accountBinding: digest(['lenovo_pro_us', options.customerId]), runId: randomUUID(),
    startedAt: options.now.toISOString(), finishedAt: new Date().toISOString(), scope,
    coverage: { complete: true, ordersListed: initial.orders.length, ordersCollected: orders.length,
      listPages: initial.pages + final.pages, listPasses: 2 }, orders };
}

export function snapshotDigest(snapshot: Snapshot): string {
  // Observation times/run IDs change on repeat; source facts and scope do not.
  return digest([snapshot.schemaVersion, snapshot.accountBinding, snapshot.scope, snapshot.orders]);
}
export interface Change { orderId: string; lineKey: string | null; kind: 'order_added' | 'order_changed' | 'line_added' | 'line_changed' }
export function compare(previous: Snapshot | null, next: Snapshot): Change[] {
  if (!previous) return []; // A baseline is not a fabricated transition.
  requireThat(previous.accountBinding === next.accountBinding, 'account_mismatch');
  const old = new Map(previous.orders.map(o => [o.orderId, o])), current = new Map(next.orders.map(o => [o.orderId, o]));
  // Aging out of a moving date window is expected. Unexplained disappearance of
  // an in-scope order is not evidence of cancellation or delivery.
  for (const order of previous.orders) if (order.placedDate >= next.scope.dateFrom && order.placedDate <= next.scope.dateTo)
    requireThat(current.has(order.orderId), 'incomplete_scope');
  const changes: Change[] = [];
  for (const order of next.orders) {
    const before = old.get(order.orderId);
    if (!before) { changes.push({ orderId: order.orderId, lineKey: null, kind: 'order_added' }); continue; }
    if (before.sourceStatus !== order.sourceStatus) changes.push({ orderId: order.orderId, lineKey: null, kind: 'order_changed' });
    const oldLines = new Map(before.lines.map(l => [l.key, l])), newLines = new Map(order.lines.map(l => [l.key, l]));
    for (const line of before.lines) requireThat(newLines.has(line.key), 'incomplete_scope');
    for (const line of order.lines) {
      const prior = oldLines.get(line.key);
      if (prior?.shipments?.length) requireThat(line.shipments !== null && line.shipments.length >= prior.shipments.length, 'incomplete_scope');
      for (const shipment of prior?.shipments ?? []) if (shipment.trackingLink?.status === 'available') {
        const next = line.shipments?.find(s => s.key === shipment.key);
        requireThat(next?.trackingLink.status === 'available', 'incomplete_scope');
      }
      // 0.1 did not collect links. Enriching its baseline is newly observed
      // coverage, not evidence that the supplier changed every shipment today.
      const facts = (value: typeof line) => previous.connectorVersion === '0.1.0'
        ? { ...value, shipments: value.shipments?.map(({ trackingLink: _link, ...rest }) => rest) ?? value.shipments }
        : value;
      if (!prior || digest(facts(prior)) !== digest(facts(line))) changes.push({ orderId: order.orderId, lineKey: line.key, kind: prior ? 'line_changed' : 'line_added' });
    }
  }
  return changes;
}
