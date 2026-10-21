import test from 'node:test';
import assert from 'node:assert/strict';
import { trackingLink } from '../src/tracking.ts';
import { parseOrder } from '../src/model.ts';
import { collect, compare } from '../src/collector.ts';
import { Failure } from '../src/errors.ts';
import { allowedRequest } from '../src/browser.ts';

const number = '1Z0000000000000000'; // Invented tracking identifier.
const legacy = `http://wwwapps.ups.com/WebTracking/track?HTMLVersion=5.0&loc=en_US&Requester=UPSHome&WBPM_lid=homepage%2Fct1.html_pnl_trk&trackNums=${number}&track.x=Track`;
const safe = `https://wwwapps.ups.com/WebTracking/track?track=yes&trackNums=${number}`;
const ref = (id: string) => ({ orderId: id, orderNumber: `FICT-${id}`, customerId: 'FICT-CUSTOMER', placedDate: '2026-10-01', sourceStatus: '2' });
function raw(id: string) {
  const r = ref(id);
  return { status: 200, data: { customerId: r.customerId, eCommerceId: r.orderId, orderNumber: r.orderNumber, placeOrderDate: '10/01/2026', orderStatus: 2,
    orderItemsVO: [{ orderNumber: r.orderNumber, itemId: 1, itemNo: 1, lineNo: 1, quantity: 1, productNumber: 'FICT-PC', productDesc: 'Invented computer',
      shippingStatusVOList: [{ carrier: 'UPS', trackingNumber: number, trackingUrl: legacy, quantity: 1 }] }] } };
}
const options = { customerId: ref('a').customerId, timezone: 'America/Chicago', now: new Date('2026-10-20T19:00:00Z'), signal: new AbortController().signal, maxPages: 1, maxOrders: 5 };
async function snapshot() {
  return collect({ list: async () => ({ status: 200, data: { page: { pageNum: 1, pageSize: 5, totalCount: 1, totalPageNum: 1, version: 1,
    datas: [{ eCommerceId: 'a', orderNumber: ref('a').orderNumber, customerId: options.customerId, placeOrderDate: '10/01/2026', orderStatus: 2 }] } } }), detail: async () => raw('a') }, options);
}
test('observed UPS formats become one HTTPS link with only the matching package number', () => {
  assert.deepEqual(trackingLink(legacy, number, 'UPS'), { status: 'available', carrier: 'UPS', url: safe });
  assert.equal(trackingLink(safe, number, 'UPS').url, safe);
  assert.equal(trackingLink(`http://wwwapps.ups.com/WebTracking/track?track=yes&trackNums=${number}`, number, 'UPS').url, safe);
  assert.equal(allowedRequest(safe, 'GET'), false); // Links never add a carrier runtime dependency.
});
test('unsafe, credentialized and unreviewed links never become clickable or persist their values', () => {
  for (const value of [undefined, null, '']) assert.equal(trackingLink(value, number, 'UPS').status, 'not_provided');
  for (const value of ['javascript:alert(1)', 'file:///etc/passwd', 'http://127.0.0.1/track', 'https://wwwapps.ups.com.evil.invalid/WebTracking/track',
    `https://u:p@wwwapps.ups.com/WebTracking/track?trackNums=${number}`, safe + '#session-secret', safe + '&session=DO-NOT-PERSIST',
    safe + '&trackNums=OTHER', safe.replace('wwwapps.ups.com', 'wwwapps.ups.com:8443'), safe + '%0a', 'https:\\wwwapps.ups.com\\WebTracking\\track', safe.replace('https://', 'https:/'), 123]) {
    const result = trackingLink(value, number, 'UPS');
    assert.notEqual(result.status, 'available'); assert.equal(result.url, null); assert.equal(JSON.stringify(result).includes('DO-NOT-PERSIST'), false);
  }
  assert.equal(trackingLink(safe, '1Z1111111111111111', 'UPS').status, 'number_mismatch');
  assert.equal(trackingLink(safe, null, 'UPS').status, 'number_mismatch');
  assert.equal(trackingLink(safe, number, 'Other carrier').status, 'unreviewed');
});
test('tracking links stay on their exact order, source line and shipment across reorder and ETA changes', () => {
  const first = parseOrder(raw('a'), ref('a')), other = parseOrder(raw('b'), ref('b'));
  const link = first.linkedTrackingInformation[0], line = first.lines[0], shipment = line.shipments![0];
  assert.equal(link.label, 'UPS tracking'); assert.equal(link.url, safe); assert.equal(link.lineKey, line.key); assert.equal(link.shipmentKey, shipment.key);
  assert.equal(link.source, 'lenovo_order_detail'); assert.equal(link.trackingNumber, shipment.trackingNumber);
  assert.notEqual(link.lineKey, other.linkedTrackingInformation[0].lineKey);
  assert.notEqual(link.shipmentKey, other.linkedTrackingInformation[0].shipmentKey);
  const changed = raw('a'); changed.data.orderItemsVO[0].shippingStatusVOList[0].trackingUrl = safe;
  assert.deepEqual(parseOrder(changed, ref('a')), first);
});
test('tracking enrichment reads legacy history without invented changes and tracks later updates', async () => {
  const next = await snapshot(), before = structuredClone(next); before.connectorVersion = '0.1.0';
  for (const order of before.orders) {
    Reflect.deleteProperty(order, 'linkedTrackingInformation');
    for (const line of order.lines) for (const shipment of line.shipments ?? []) Reflect.deleteProperty(shipment, 'trackingLink');
  }
  assert.deepEqual(compare(before, next), []);
  const updated = structuredClone(next);
  updated.orders[0].lines[0].shipments![0].trackingLink.url += '&requester=ST';
  assert.deepEqual(compare(next, updated).map(c => c.kind), ['line_changed']);
  const missing = structuredClone(next); missing.orders[0].lines[0].shipments![0].trackingLink = trackingLink(null, number, 'UPS');
  assert.throws(() => compare(next, missing), (error: unknown) => error instanceof Failure && error.code === 'incomplete_scope');
});
