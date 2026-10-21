import test from 'node:test';
import assert from 'node:assert/strict';
import { collect, compare, snapshotDigest, withAuthenticationRecovery } from '../src/collector.ts';
import type { Options, Snapshot, Transport } from '../src/collector.ts';
import { parseOrder, rollingScope, sourceDate } from '../src/model.ts';
import type { OrderRef } from '../src/model.ts';
import { Failure } from '../src/errors.ts';
import { allowedRequest, retryAfter } from '../src/browser.ts';
import { parseConfig } from '../src/config.ts';
import { parseSourceJson } from '../src/json.ts';

// Invented source records only. The private source is never a test fixture.
const customer = 'fictional-customer';
const ref = (id = 'order-a', date = '2026-10-01'): OrderRef => ({ orderId: id, orderNumber: `number-${id}`, customerId: customer, placedDate: date, sourceStatus: '2' });
function detail(r = ref()) {
  return { status: 200, data: { customerId: customer, eCommerceId: r.orderId, orderNumber: r.orderNumber,
    placeOrderDate: `${r.placedDate.slice(5, 7)}/${r.placedDate.slice(8)}/${r.placedDate.slice(0, 4)}`, orderStatus: 2,
    email: 'DO-NOT-PERSIST@example.invalid', orderAddress: [{ street: 'DO-NOT-PERSIST' }], orderPaysVO: [{ token: 'DO-NOT-PERSIST' }],
    recommendations: [{ productDesc: 'Unpurchased recommendation' }],
    orderItemsVO: [{ orderNumber: r.orderNumber, itemId: 10, itemNo: 10, lineNo: 1, quantity: 2, shippedQuantity: 1,
      cancelledQuantity: 0, productNumber: 'FICT-PC', productDesc: 'Invented computer', orderStatus: 'processing', orderStatusName: 'Processing', physicalProduct: 1, isDigitalProduct: false,
      estimatedDeliveryDate: '2026-10-12 00:00:00', estimatedShipDate: '2026-10-08', upsDeliveryDate: '2026-10-13',
      shippingStatusVOList: [{ carrier: 'Invented carrier', trackingNumber: 'FICT-PACKAGE', quantity: 1,
        estimatedArrivalDate: '10/13/2026', deliveredDate: '', trackingUrl: 'https://unapproved.invalid/private-token', delay: false }],
      componentItems: [{ orderNumber: r.orderNumber, itemId: 11, itemNo: 11, lineNo: 2, quantity: 2,
        productNumber: 'FICT-SERVICE', productDesc: 'Invented warranty', orderStatus: 'fulfilled', orderStatusName: 'Fulfilled',
        physicalProduct: 0, isDigitalProduct: true, shippingStatusVOList: [] }], splitItems: [] }] } };
}
function page(rows: OrderRef[], number = 1, size = 2, total = rows.length) {
  return { status: 200, data: { page: { pageNum: number, pageSize: size, totalCount: total, totalPageNum: Math.ceil(total / size), version: 1,
    datas: rows.map(r => ({ eCommerceId: r.orderId, orderNumber: r.orderNumber, customerId: r.customerId, orderStatus: 2,
      placeOrderDate: `${r.placedDate.slice(5, 7)}/${r.placedDate.slice(8)}/${r.placedDate.slice(0, 4)}` })) } } };
}
const options = (): Options => ({ customerId: customer, timezone: 'America/Chicago', now: new Date('2026-10-20T19:00:00Z'),
  signal: new AbortController().signal, maxPages: 100, maxOrders: 1000, sleep: async () => {} });
function transport(rows = [ref()]): Transport {
  return { list: async n => page(rows.slice((n - 1) * 2, n * 2), n, 2, rows.length), detail: async r => detail(r) };
}
const fails = (code: string) => (error: unknown) => error instanceof Failure && error.code === code;

test('90 calendar dates use the account timezone at midnight, including leap years', () => {
  assert.deepEqual(rollingScope(new Date('2026-10-21T02:00:00Z'), 'America/Chicago'),
    { kind: 'placed_date', dateFrom: '2026-07-23', dateTo: '2026-10-20', days: 90, timezone: 'America/Chicago' });
  assert.equal(sourceDate('02/29/2024'), '2024-02-29');
  assert.throws(() => sourceDate('02/29/2026'), fails('source_schema_invalid'));
  assert.throws(() => sourceDate('soon'), fails('source_schema_invalid'));
});
test('large numeric supplier IDs and fractional quantities retain their exact JSON tokens', () => {
  const source = JSON.stringify(detail()).replace('"itemId":10', '"itemId":9876543210123456789');
  const order = parseOrder(parseSourceJson(source), ref());
  assert.equal(order.lines.find(l => l.relation === 'item')!.sourceItemId, '9876543210123456789');
  assert.deepEqual(parseSourceJson('{"quantity":0.123456789,"safe":2}'), { quantity: '0.123456789', safe: 2 });
});
test('walks multiple pages twice and requests details only within the exact window', async () => {
  const rows = [ref('old', '2026-07-22'), ref('start', '2026-07-23'), ref('recent'), ref('future', '2026-10-21'), ref('end', '2026-10-20')];
  const calls: number[] = [], fetched: string[] = [], base = transport(rows);
  const result = await collect({ list: async n => { calls.push(n); return base.list(n); }, detail: async r => { fetched.push(r.orderId); return base.detail(r); } }, options());
  assert.deepEqual(calls, [1, 2, 3, 1, 2, 3]); assert.equal(result.coverage.ordersListed, 5);
  assert.deepEqual(fetched, ['end', 'recent', 'start']); assert.equal(result.orders.length, 3);
});
test('keeps service fulfillment separate from physical delivery and strips unrelated fields', () => {
  const parsed = parseOrder(detail(), ref());
  const service = parsed.lines.find(l => l.relation === 'component')!;
  assert.equal(service.status.label, 'Fulfilled'); assert.deepEqual(service.shipments, []); assert.equal(service.shippedQuantity, null);
  const shipment = parsed.lines.find(l => l.relation === 'item')!.shipments![0];
  assert.equal(shipment.estimatedArrivalDate, '2026-10-13'); assert.equal(shipment.deliveredDate, null);
  assert.equal(JSON.stringify(parsed).includes('DO-NOT-PERSIST'), false);
  assert.equal(JSON.stringify(parsed).includes('private-token'), false);
  assert.equal(JSON.stringify(parsed).includes('Unpurchased'), false);
});
test('source line/shipment identity remains stable when ETA changes', () => {
  const first = parseOrder(detail(), ref()), changed = detail();
  changed.data.orderItemsVO[0].shippingStatusVOList[0].estimatedArrivalDate = '10/15/2026';
  const second = parseOrder(changed, ref());
  assert.deepEqual(first.lines.map(l => l.key), second.lines.map(l => l.key));
  assert.equal(first.lines.find(l => l.relation === 'item')!.shipments![0].key, second.lines.find(l => l.relation === 'item')!.shipments![0].key);
});
test('preserves distinct shipment dates and rejects duplicate identifiable shipments', () => {
  const body = detail();
  body.data.orderItemsVO[0].shippingStatusVOList.push({ ...body.data.orderItemsVO[0].shippingStatusVOList[0], trackingNumber: 'FICT-OTHER', estimatedArrivalDate: '10/15/2026' });
  assert.equal(parseOrder(body, ref()).lines.find(l => l.relation === 'item')!.shipments!.length, 2);
  body.data.orderItemsVO[0].shippingStatusVOList[1].trackingNumber = 'FICT-PACKAGE';
  assert.throws(() => parseOrder(body, ref()), fails('source_schema_invalid'));
});
test('an arrival label without a year is retained without inventing a date', () => {
  const body = detail(); body.data.orderItemsVO[0].shippingStatusVOList[0].estimatedArrivalDate = 'Get it by Thu. Oct 22';
  const shipment = parseOrder(body, ref()).lines.find(l => l.relation === 'item')!.shipments![0];
  assert.equal(shipment.estimatedArrivalDate, null);
  assert.equal(shipment.estimatedArrivalLabel, 'Get it by Thu. Oct 22');
  assert.equal(shipment.arrivalYearMissing, true);
});
test('unknown quantities stay unknown; contradictory quantities and missing IDs fail closed', () => {
  const body = detail(); body.data.orderItemsVO[0].shippedQuantity = 3;
  assert.throws(() => parseOrder(body, ref()), fails('source_schema_invalid'));
  const missing = detail() as unknown as { data: { orderItemsVO: Record<string, unknown>[] } };
  delete missing.data.orderItemsVO[0].itemId;
  assert.throws(() => parseOrder(missing, ref()), fails('source_schema_invalid'));
});
test('rejects wrong customer and mismatched order identities', async () => {
  const wrong = ref(); wrong.customerId = 'another-account';
  await assert.rejects(collect(transport([wrong]), options()), fails('account_mismatch'));
  const t = transport(); t.detail = async () => detail(ref('not-requested'));
  await assert.rejects(collect(t, options()), fails('account_mismatch'));
});
test('rejects repeated pages, duplicate identities and page-count drift', async () => {
  await assert.rejects(collect({ list: async () => page([ref(), ref('b')], 1, 2, 3), detail: async r => detail(r) }, options()), fails('incomplete_scope'));
  await assert.rejects(collect(transport([ref(), ref()]), options()), fails('incomplete_scope'));
  const t = transport([ref(), ref('b'), ref('c')]); const original = t.list;
  t.list = async n => n === 2 ? page([ref('c')], 2, 2, 4) : original(n);
  await assert.rejects(collect(t, options()), fails('incomplete_scope'));
});
test('detects list changes during detail retrieval and enforces size limits', async () => {
  let reads = 0; const t = transport(); t.list = async () => page([ref(reads++ === 0 ? 'first' : 'changed')]);
  await assert.rejects(collect(t, options()), fails('incomplete_scope'));
  await assert.rejects(collect(transport([ref(), ref('b'), ref('c')]), { ...options(), maxPages: 1 }), fails('incomplete_scope'));
});
test('bounded retry honors transient/rate-limit responses; authentication never retries', async () => {
  const t = transport(), original = t.list; let count = 0; const waits: number[] = [];
  t.list = async n => { if (++count === 1) throw new Failure('rate_limited', 1500); return original(n); };
  await collect(t, { ...options(), sleep: async ms => { waits.push(ms); } }); assert.deepEqual(waits, [1500]);
  count = 0; t.list = async () => { count++; throw new Failure('authentication_required'); };
  await assert.rejects(collect(t, options()), fails('authentication_required')); assert.equal(count, 1);
  t.list = async () => { throw new Failure('rate_limited', 60000); };
  await assert.rejects(collect(t, options()), fails('rate_limited'));
});
test('repeat is unchanged, ETA changes create one line change, disappearance is not cancellation', async () => {
  const first = await collect(transport(), options()), second = await collect(transport(), options());
  assert.equal(snapshotDigest(first), snapshotDigest(second)); assert.deepEqual(compare(first, second), []); assert.deepEqual(compare(null, first), []);
  const changed = structuredClone(second); changed.orders[0].lines.find(l => l.relation === 'item')!.shipments![0].estimatedArrivalDate = '2026-10-17';
  assert.deepEqual(compare(first, changed).map(c => c.kind), ['line_changed']);
  const missing: Snapshot = { ...second, orders: [] };
  assert.throws(() => compare(first, missing), fails('incomplete_scope'));
  missing.scope = { ...missing.scope, dateFrom: '2026-10-02' }; assert.deepEqual(compare(first, missing), []);
  const lostLine = structuredClone(second); lostLine.orders[0].lines.pop();
  assert.throws(() => compare(first, lostLine), fails('incomplete_scope'));
  const lostShipment = structuredClone(second); lostShipment.orders[0].lines.find(l => l.relation === 'item')!.shipments = [];
  assert.throws(() => compare(first, lostShipment), fails('incomplete_scope'));
});
test('cancellation stops before the next read', async () => {
  const controller = new AbortController(); controller.abort();
  await assert.rejects(collect(transport(), { ...options(), signal: controller.signal }), fails('collection_cancelled'));
});
test('authentication recovery restarts once and never retries account/schema failures', async () => {
  let attempts = 0, renewals = 0;
  const signal = new AbortController().signal;
  const value = await withAuthenticationRecovery(async () => { if (++attempts === 1) throw new Failure('authentication_required'); return 'complete'; }, async () => { renewals++; }, signal);
  assert.equal(value, 'complete'); assert.equal(attempts, 2); assert.equal(renewals, 1);
  attempts = 0;
  await assert.rejects(withAuthenticationRecovery(async () => { attempts++; throw new Failure('authentication_required'); }, async () => {}, signal), fails('authentication_required'));
  assert.equal(attempts, 2);
  await assert.rejects(withAuthenticationRecovery(async () => { throw new Failure('account_mismatch'); }, async () => { assert.fail('must not renew'); }, signal), fails('account_mismatch'));
});
test('egress policy blocks mutations, metadata, alternate ports, credentials and logout', () => {
  assert.equal(allowedRequest('https://openapi.lenovo.com/us/lenovopro/en/v1/refreshToken', 'POST'), true);
  assert.equal(allowedRequest('https://openapi.lenovo.com/us/lenovopro/en/v1/refreshToken', 'OPTIONS'), true);
  assert.equal(allowedRequest('https://openapi.lenovo.com/us/lenovopro/en/createOrder', 'GET'), false);
  assert.equal(allowedRequest('https://account.lenovo.com/anything', 'GET'), false);
  for (const [url, method] of [['https://openapi.lenovo.com/order/cancel', 'GET'], ['https://account.lenovo.com/logout', 'GET'],
    ['https://openapi.lenovo.com/cart', 'POST'], ['http://account.lenovo.com/', 'GET'], ['https://169.254.169.254/', 'GET'],
    ['https://account.lenovo.com:8443/', 'GET'], ['https://u:p@account.lenovo.com/', 'GET']]) assert.equal(allowedRequest(url, method), false);
  assert.equal(retryAfter('60'), 60000);
  assert.throws(() => parseConfig({ schema: 1, customerId: customer, timezone: 'America/Chicago', days: 10 }), fails('configuration_invalid'));
});
