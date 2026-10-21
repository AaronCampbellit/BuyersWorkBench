import { chromium } from 'playwright';
import type { Browser, BrowserContext, Page } from 'playwright';
import { createServer, createConnection } from 'node:net';
import type { Server } from 'node:net';
import { Failure, requireThat, safeFailure } from './errors.ts';
import { object } from './model.ts';
import type { OrderRef } from './model.ts';
import type { Transport } from './collector.ts';
import type { Session } from './session.ts';
import { parseSourceJson } from './json.ts';

export const LIST_PAGE = 'https://account.lenovo.com/us/lenovopro/en/account/order/list.html';
export const LIST_PATH = '/us/lenovopro/en/ofp/auth/mobileOrder/list';
export const DETAIL_PATH = '/us/lenovopro/en/ofp/order/authDetail';
export const HOSTS = new Set(['account.lenovo.com', 'openapi.lenovo.com', 'www.lenovo.com', 'passport.lenovo.com', 'bpsso.lenovo.com',
  'ap-ss.static.pub', 'ap-img.static.pub', 'p1-ofp.static.pub', 'p2-ofp.static.pub', 'p3-ofp.static.pub', 'p4-ofp.static.pub']);
const posts = new Set(['/us/lenovopro/en/dict/getdictinfobykey', '/us/lenovopro/en/v1/refreshToken', '/us/lenovopro/en/smbPro/refreshSmbToken']);
const apiReads = new Set([LIST_PATH, DETAIL_PATH, ...[
  '_auto_jump_/simulated/auth/jump', 'address/api/getTemplateInfo', 'api/sscmssite/fragment/getFragmentContentBatch',
  'api/sscmssite/utility/getUtilityListById', 'auth/assign_cookie', 'dict/order/reordertoggle', 'dict/pquery',
  'familyhub/v1/status', 'getGroupInfo', 'invoke/v1/getUserFamilyHubInfo', 'lenovoUniverse/getLUStatus',
  'ofp/order/getItdmUsers', 'ofp/search/dlp/cache/maxSave/get/_tsc', 'ofp/search/suggest/selector', 'select/selectList',
  'smb/community/checkAgreeToCommunity', 'smbHome/v1/smbHeader', 'smbPro/getSmbMdmToggle', 'smbweb/smbToggleStatus',
  'v1/company/selectDictionaryByCountry', 'v1/company/selectDictionaryByTypeId', 'v1/config/listAutofillEmail',
  'v1/home/getMastheadloyalty', 'v1/profile', 'home/v1/getMFAToggleStatus',
].map(path => '/us/lenovopro/en/' + path)]);
export function allowedRequest(raw: string, method: string, resourceType = 'fetch'): boolean {
  let u: URL; try { u = new URL(raw); } catch { return false; }
  if (u.protocol !== 'https:' || u.port && u.port !== '443' || u.username || u.password || !HOSTS.has(u.hostname)) return false;
  if (/logout|signout|clear_cookie|cancel|refund|checkout|addtocart|placeorder|deleteaccount/i.test(u.pathname) || /logout/i.test(u.searchParams.get('lenovoid.action') ?? '')) return false;
  if (method === 'POST') return u.hostname === 'openapi.lenovo.com' && posts.has(u.pathname);
  if (!['GET', 'HEAD', 'OPTIONS'].includes(method)) return false;
  if (method === 'OPTIONS' && u.hostname === 'openapi.lenovo.com' && posts.has(u.pathname)) return true;
  if (u.hostname === 'openapi.lenovo.com') return apiReads.has(u.pathname);
  if (u.hostname.endsWith('.static.pub')) return true;
  if (['passport.lenovo.com', 'bpsso.lenovo.com'].includes(u.hostname)) return u.pathname.startsWith('/glbwebauthnv6/');
  if (u.pathname === '/store-alert-storage.html' || u.pathname === '/us/lenovopro/en/' ||
    u.hostname === 'account.lenovo.com' && ['/us/lenovopro/en/account/order/list.html', '/us/lenovopro/en/account/order/detail.html', '/us/lenovopro/en/smbaccount/gatekeeper/showpage'].includes(u.pathname)) return true;
  // Lenovo's observed anti-bot script has an opaque rotating path. It is a
  // script resource on the account/www hosts, never an API fetch/navigation.
  return resourceType === 'script' && /^\/[A-Za-z0-9_-]{16,}\/[A-Za-z0-9_-]{8,}\//.test(u.pathname);
}
export function retryAfter(raw: string | null, now = Date.now()): number {
  if (!raw) return 0;
  if (/^\d+$/.test(raw)) return Math.min(300_000, Number(raw) * 1000);
  const timestamp = Date.parse(raw); return Number.isFinite(timestamp) ? Math.max(0, Math.min(300_000, timestamp - now)) : 0;
}
function responseFailure(status: number, after: string | null): void {
  if (status === 401) throw new Failure('authentication_required');
  if (status === 403) throw new Failure('permission_denied');
  if (status === 429) throw new Failure('rate_limited', retryAfter(after));
  if (status >= 500) throw new Failure('source_unavailable');
  requireThat(status === 200);
}
export class LenovoBrowser implements Transport {
  readonly browser: Browser;
  readonly context: BrowserContext;
  private readonly page: Page;
  private readonly bridge: Server;
  private template: URL | null = null;
  private blocked = 0;
  private renewals = 0;
  private readonly pageSize: number;
  private constructor(browser: Browser, context: BrowserContext, page: Page, bridge: Server, pageSize: number) {
    this.browser = browser; this.context = context; this.page = page; this.bridge = bridge; this.pageSize = pageSize;
  }
  static async open(session: Session, pageSize = 25): Promise<LenovoBrowser> {
    requireThat(Number.isSafeInteger(pageSize) && pageSize >= 1 && pageSize <= 100, 'configuration_invalid');
    const bridge = createServer(client => {
      const upstream = createConnection('/egress/proxy.sock');
      client.on('error', () => upstream.destroy()); upstream.on('error', () => client.destroy());
      client.pipe(upstream).pipe(client);
    });
    let browser: Browser | undefined;
    try {
      await new Promise<void>((resolve, reject) => { bridge.once('error', reject); bridge.listen(18080, '127.0.0.1', resolve); });
      browser = await chromium.launch({ headless: false, chromiumSandbox: true, proxy: { server: 'http://127.0.0.1:18080' }, timeout: 25000 });
      const context = await browser.newContext({ storageState: session.storageState, locale: session.locale,
        timezoneId: session.timezone, serviceWorkers: 'block', acceptDownloads: false });
      await context.addInitScript(({ origin, values }) => {
        if (location.origin === origin) for (const [key, value] of Object.entries(values)) window.sessionStorage.setItem(key, value);
      }, session.sessionStorage);
      const page = await context.newPage();
      const client = new LenovoBrowser(browser, context, page, bridge, pageSize);
      context.on('response', response => {
        const u = new URL(response.url());
        if (u.hostname === 'openapi.lenovo.com' && response.request().method() === 'POST' && posts.has(u.pathname) && /refresh/.test(u.pathname) && response.status() === 200) client.renewals++;
      });
      await context.route('**/*', route => {
        const request = route.request();
        if (!allowedRequest(request.url(), request.method(), request.resourceType())) { client.blocked++; return route.abort('blockedbyclient'); }
        return route.continue();
      });
      return client;
    } catch {
      await browser?.close().catch(() => {}); bridge.close(); throw new Failure('network_error');
    }
  }
  async initialize(): Promise<void> {
    for (let attempt = 0; attempt < 2; attempt++) {
      const pending = this.page.waitForResponse(r => {
        const u = new URL(r.url()); return u.hostname === 'openapi.lenovo.com' && u.pathname === LIST_PATH && r.request().method() === 'GET';
      }, { timeout: 25000 }).catch(() => null);
      const navigation = await this.page.goto(LIST_PAGE, { waitUntil: 'domcontentloaded', timeout: 30000 }).catch(() => null);
      const response = await pending;
      if (response) {
        if (response.status() === 401 && attempt === 0) { await this.renewSession(); continue; }
        responseFailure(response.status(), response.headers()['retry-after'] ?? null);
        this.template = new URL(response.url());
        // Preserve the observed channel/sort parameters while explicitly clearing
        // known UI filters. Fetch all pages, then apply the exact 90-date window.
        for (const name of ['orderPeriod', 'searchByContent', 'orderItems', 'customOrderStatus', 'placeBy', 'storeName']) this.template.searchParams.set(name, '');
        this.template.searchParams.set('pageSize', String(this.pageSize));
        return;
      }
      const password = await this.page.locator('input[type="password"]').first().isVisible().catch(() => false);
      const challenge = await this.page.locator('iframe[src*="captcha"], [id*="captcha"]:visible').count().catch(() => 0);
      if (challenge) throw new Failure('challenge_required');
      if (password) throw new Failure('authentication_required');
      if (!navigation) throw new Failure('network_error');
      responseFailure(navigation.status(), null);
      if (attempt === 0) {
        await this.renewSession();
      }
    }
    throw new Failure(/gatekeeper|login|passport/.test(new URL(this.page.url()).pathname) ? 'authentication_required' : 'source_unavailable');
  }
  async renewSession(): Promise<void> {
    // Only the website's observed SSO function; no credential entry or challenge
    // submission. The outer collection restarts rather than mixing partial runs.
    const pending = this.page.waitForResponse(r => new URL(r.url()).hostname === 'openapi.lenovo.com' && /\/(refreshToken|refreshSmbToken)$/.test(new URL(r.url()).pathname),
      { timeout: 12000 }).catch(() => null);
    const available = await this.page.evaluate(() => {
      const api = (window as unknown as { flash_fe_core_tool?: { $Api?: { syncSSO?: () => void } } }).flash_fe_core_tool?.$Api;
      if (typeof api?.syncSSO !== 'function') return false;
      api.syncSSO(); return true;
    }).catch(() => false);
    const response = await pending;
    if (!available || !response) throw new Failure('authentication_required');
    responseFailure(response.status(), null);
  }
  private async fetch(url: URL): Promise<unknown> {
    requireThat(url.origin === 'https://openapi.lenovo.com' && [LIST_PATH, DETAIL_PATH].includes(url.pathname), 'configuration_invalid');
    try {
      const result = await this.page.evaluate(async url => {
        const response = await fetch(url, { credentials: 'include', redirect: 'error', signal: AbortSignal.timeout(20000), cache: 'no-store' });
        const reader = response.body?.getReader(), chunks: Uint8Array[] = []; let bytes = 0;
        if (reader) while (true) {
          const chunk = await reader.read(); if (chunk.done) break;
          bytes += chunk.value.length;
          if (bytes > 8 * 1024 * 1024) { await reader.cancel(); return { status: response.status, tooLarge: true, text: '', after: null }; }
          chunks.push(chunk.value);
        }
        const buffer = new Uint8Array(bytes); let offset = 0;
        for (const chunk of chunks) { buffer.set(chunk, offset); offset += chunk.length; }
        return { status: response.status, tooLarge: false, text: new TextDecoder().decode(buffer), after: response.headers.get('retry-after') };
      }, url.toString());
      responseFailure(result.status, result.after);
      requireThat(!result.tooLarge);
      const body = parseSourceJson(result.text);
      const root = object(body);
      if (typeof root.status === 'number') responseFailure(root.status, null);
      return body;
    } catch (error) { throw safeFailure(error, 'network_error'); }
  }
  async list(page: number): Promise<unknown> {
    requireThat(this.template && Number.isSafeInteger(page) && page > 0, 'configuration_invalid');
    const url = new URL(this.template); url.searchParams.set('pageNum', String(page));
    return this.fetch(url);
  }
  async detail(order: OrderRef): Promise<unknown> {
    const url = new URL(DETAIL_PATH, 'https://openapi.lenovo.com');
    url.searchParams.set('ecommerceId', order.orderId); url.searchParams.set('customerIdInfo', order.customerId); url.searchParams.set('smb', 'true');
    return this.fetch(url);
  }
  blockedCount(): number { return this.blocked; }
  renewalCount(): number { return this.renewals; }
  async close(): Promise<void> { await this.browser.close().catch(() => {}); this.bridge.close(); }
}
