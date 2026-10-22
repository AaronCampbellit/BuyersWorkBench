// Optional local-browser check; uses only invented state and makes no requests.
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { chromium } from 'playwright';
import { decodeScrapeyy, scrapeyyLenovoSession } from '../src/scrapeyy-import.ts';

const bundle = decodeScrapeyy(readFileSync(new URL('./fixtures/scrapeyy/plaintext.json', import.meta.url)));
bundle.cookies[0].sameSite = 'unspecified';
const session = scrapeyyLenovoSession(bundle, 'FICT-EXPECTED-ACCOUNT');
const browser = await chromium.launch({ headless: true });
try {
  const context = await browser.newContext({ storageState: session.storageState });
  await context.route('**/*', route => route.abort());
  const page = await context.newPage(), cdp = await context.newCDPSession(page);
  // Use the browser's URL matcher: Playwright's convenience cookie filter
  // matches subdomains even for host-only cookies and cannot prove this property.
  const exact = (await cdp.send('Network.getCookies', { urls: ['https://account.lenovo.com/'] })).cookies;
  const child = (await cdp.send('Network.getCookies', { urls: ['https://child.account.lenovo.com/'] })).cookies;
  assert.equal(exact.length, 1); assert.equal(child.length, 0);
  assert.equal(exact[0].httpOnly, true); assert.equal(exact[0].session, true);
  assert.equal(exact[0].domain, 'account.lenovo.com');
  console.log(JSON.stringify({ outcome: 'passed', hostOnlyPreserved: true,
    httpOnlyPreserved: true, sessionLifetimePreserved: true, supplierRequests: 0 }));
} finally { await browser.close(); }
