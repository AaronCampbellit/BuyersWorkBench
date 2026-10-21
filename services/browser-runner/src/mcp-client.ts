import { Failure, requireThat } from './errors.ts';
import { object } from './model.ts';

export function localMcpUrl(value: string): URL {
  let url: URL; try { url = new URL(value); } catch { throw new Failure('configuration_invalid'); }
  requireThat(url.protocol === 'http:' && ['127.0.0.1', 'localhost', '[::1]'].includes(url.hostname) && url.pathname === '/mcp' && !url.search && !url.hash && !url.username && !url.password, 'configuration_invalid');
  return url;
}
export class LocalMcp {
  private readonly url: URL;
  private session: string | null = null;
  private protocol = '2025-03-26';
  private nextId = 1;
  constructor(endpoint: string) { this.url = localMcpUrl(endpoint); }
  private async request(method: string, params: unknown, notification = false): Promise<Record<string, unknown>> {
    const id = notification ? undefined : this.nextId++;
    try {
      const headers: Record<string, string> = { 'Content-Type': 'application/json', Accept: 'application/json, text/event-stream', 'MCP-Protocol-Version': this.protocol };
      if (this.session) headers['Mcp-Session-Id'] = this.session;
      const response = await fetch(this.url, { method: 'POST', headers, body: JSON.stringify({ jsonrpc: '2.0', id, method, params }),
        redirect: 'error', signal: AbortSignal.timeout(method === 'tools/call' ? 180000 : 30000) });
      requireThat(response.ok, 'network_error');
      if (method === 'initialize') this.session = response.headers.get('Mcp-Session-Id');
      if (notification) { await response.body?.cancel(); return {}; }
      let reply: Record<string, unknown> | null = null;
      if (response.headers.get('content-type')?.includes('text/event-stream')) {
        const reader = response.body!.getReader(), decoder = new TextDecoder(); let buffer = '', size = 0;
        try { while (!reply) {
          const part = await reader.read(); if (part.done) break;
          size += part.value.length; requireThat(size <= 8 * 1024 * 1024, 'network_error');
          buffer = (buffer + decoder.decode(part.value, { stream: true })).replace(/\r\n/g, '\n');
          let boundary: number;
          while ((boundary = buffer.indexOf('\n\n')) >= 0) {
            const event = buffer.slice(0, boundary); buffer = buffer.slice(boundary + 2);
            const data = event.split('\n').filter(line => line.startsWith('data:')).map(line => line.slice(5).trimStart()).join('\n');
            if (!data) continue;
            const value = object(JSON.parse(data)); if (value.id === id) { reply = value; break; }
          }
        } } finally { await reader.cancel().catch(() => {}); }
      } else {
        const reader = response.body!.getReader(), decoder = new TextDecoder(); let raw = '', size = 0;
        try { while (true) {
          const part = await reader.read(); if (part.done) break;
          size += part.value.length; requireThat(size <= 8 * 1024 * 1024, 'network_error');
          raw += decoder.decode(part.value, { stream: true });
        } } finally { await reader.cancel().catch(() => {}); }
        reply = object(JSON.parse(raw + decoder.decode()));
      }
      requireThat(reply && reply.id === id && !reply.error, 'network_error');
      return object(reply.result);
    } catch (error) { throw error instanceof Failure ? error : new Failure('network_error'); }
  }
  async initialize(): Promise<string> {
    const result = await this.request('initialize', { protocolVersion: this.protocol, capabilities: {}, clientInfo: { name: 'buyers-workbench-provision', version: '0.3.0' } });
    requireThat(typeof result.protocolVersion === 'string', 'network_error'); this.protocol = result.protocolVersion;
    await this.request('notifications/initialized', {}, true);
    const list = await this.request('tools/list', {});
    requireThat(Array.isArray(list.tools), 'network_error');
    const tools = list.tools.map(value => object(value).name);
    const name = tools.includes('browser_run_code_unsafe') ? 'browser_run_code_unsafe' : tools.includes('browser_run_code') ? 'browser_run_code' : null;
    requireThat(name, 'configuration_invalid'); return name;
  }
  async run(tool: string, code: string): Promise<unknown> {
    requireThat(['browser_run_code_unsafe', 'browser_run_code'].includes(tool), 'configuration_invalid');
    const result = await this.request('tools/call', { name: tool, arguments: { code } });
    requireThat(!result.isError && Array.isArray(result.content), 'authentication_required');
    for (const item of result.content) {
      const block = object(item); if (block.type !== 'text' || typeof block.text !== 'string') continue;
      const marker = block.text.indexOf('### Result\n');
      const raw = marker >= 0 ? block.text.slice(marker + 11).split('\n### ')[0].trim() : block.text.trim();
      try { const value = object(JSON.parse(raw)); if (typeof value.ok === 'boolean') return value; } catch {}
    }
    throw new Failure('state_invalid');
  }
}
