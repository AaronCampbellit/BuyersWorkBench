import { Client } from '@modelcontextprotocol/sdk/client/index.js';
import { StreamableHTTPClientTransport } from '@modelcontextprotocol/sdk/client/streamableHttp.js';
import { Failure, requireThat } from './errors.ts';
import { object } from './model.ts';

export function localMcpUrl(value: string): URL {
  let url: URL; try { url = new URL(value); } catch { throw new Failure('configuration_invalid'); }
  requireThat(url.protocol === 'http:' && ['127.0.0.1', 'localhost', '[::1]'].includes(url.hostname) && url.pathname === '/mcp' && !url.search && !url.hash && !url.username && !url.password, 'configuration_invalid');
  return url;
}
export class LocalMcp {
  private readonly url: URL;
  private readonly client = new Client({ name: 'buyers-workbench-provision', version: '0.3.0' });
  constructor(endpoint: string) { this.url = localMcpUrl(endpoint); }
  async initialize(): Promise<string> {
    try {
      const endpoint = this.url;
      const transport = new StreamableHTTPClientTransport(endpoint, {
        reconnectionOptions: { maxRetries: 0, initialReconnectionDelay: 1000, maxReconnectionDelay: 1000, reconnectionDelayGrowFactor: 1 },
        fetch: async (input, init) => {
          const url = new URL(input instanceof Request ? input.url : String(input));
          requireThat(url.href === endpoint.href, 'configuration_invalid');
          const response = await fetch(input, { ...init, redirect: 'error' });
          if (!response.body) return response;
          let bytes = 0;
          const body = response.body.pipeThrough(new TransformStream<Uint8Array, Uint8Array>({ transform(chunk, controller) {
            bytes += chunk.byteLength;
            if (bytes > 8 * 1024 * 1024) { controller.error(new Failure('network_error')); return; }
            controller.enqueue(chunk);
          } }));
          return new Response(body, { status: response.status, statusText: response.statusText, headers: response.headers });
        }
      });
      // The official client keeps the GET event stream open and answers server
      // pings. A POST-only client can lose the approved browser within seconds.
      await this.client.connect(transport, { timeout: 30000, maxTotalTimeout: 30000 });
      const list = await this.client.listTools({}, { timeout: 30000, maxTotalTimeout: 30000 });
      const names = list.tools.map(tool => tool.name);
      const name = names.includes('browser_run_code_unsafe') ? 'browser_run_code_unsafe' : names.includes('browser_run_code') ? 'browser_run_code' : null;
      requireThat(name, 'configuration_invalid'); return name;
    } catch (error) { throw error instanceof Failure ? error : new Failure('network_error'); }
  }
  async run(tool: string, code: string): Promise<unknown> {
    requireThat(['browser_run_code_unsafe', 'browser_run_code'].includes(tool), 'configuration_invalid');
    let result;
    try { result = await this.client.callTool({ name: tool, arguments: { code } }, undefined, { timeout: 180000, maxTotalTimeout: 180000 }); }
    catch { throw new Failure('network_error'); }
    requireThat(!result.isError, 'network_error');
    requireThat(Array.isArray(result.content), 'state_invalid');
    for (const item of result.content) {
      const block = object(item); if (block.type !== 'text' || typeof block.text !== 'string') continue;
      const marker = block.text.indexOf('### Result\n');
      const raw = marker >= 0 ? block.text.slice(marker + 11).split('\n### ')[0].trim() : block.text.trim();
      try { const value = object(JSON.parse(raw)); if (typeof value.ok === 'boolean') return value; } catch {}
    }
    throw new Failure('state_invalid');
  }
  async close(): Promise<void> { await this.client.close().catch(() => {}); }
}
