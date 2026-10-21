"""Temporary Unix-socket CONNECT proxy with exact hosts and public-IP checks."""
import asyncio
import ipaddress
import json
import os
import pathlib
import socket
import sys

SOCKET = pathlib.Path(sys.argv[1])
HOSTS = {
    'account.lenovo.com', 'openapi.lenovo.com', 'www.lenovo.com',
    'passport.lenovo.com', 'bpsso.lenovo.com',
    'ap-ss.static.pub', 'ap-img.static.pub',
    'p1-ofp.static.pub', 'p2-ofp.static.pub',
    'p3-ofp.static.pub', 'p4-ofp.static.pub',
}


async def pump(reader, writer):
    try:
        while data := await reader.read(65536):
            writer.write(data)
            await writer.drain()
    except (ConnectionError, asyncio.CancelledError):
        pass
    finally:
        writer.close()


async def handle(reader, writer):
    upstream = None
    try:
        raw = await asyncio.wait_for(reader.readuntil(b'\r\n\r\n'), timeout=10)
        if len(raw) > 16384:
            raise ValueError('header_limit')
        method, authority, _ = raw.split(b'\r\n', 1)[0].decode('ascii').split(' ', 2)
        host, port = authority.rsplit(':', 1)
        if method != 'CONNECT' or host not in HOSTS or port != '443':
            writer.write(b'HTTP/1.1 403 Forbidden\r\nContent-Length: 0\r\n\r\n')
            await writer.drain()
            return
        answers = await asyncio.get_running_loop().getaddrinfo(
            host, 443, type=socket.SOCK_STREAM)
        addresses = []
        for family, _, _, _, address in answers:
            ip = ipaddress.ip_address(address[0])
            if not ip.is_global:
                raise ValueError('nonpublic_destination')
            if (family, address[0]) not in addresses:
                addresses.append((family, address[0]))
        if not addresses:
            raise ValueError('no_public_destination')
        for family, ip in addresses:
            try:
                incoming, upstream = await asyncio.wait_for(
                    asyncio.open_connection(ip, 443, family=family), timeout=8)
                break
            except (OSError, asyncio.TimeoutError):
                continue
        if upstream is None:
            raise ValueError('upstream_unavailable')
        writer.write(b'HTTP/1.1 200 Connection Established\r\n\r\n')
        await writer.drain()
        await asyncio.gather(pump(reader, upstream), pump(incoming, writer))
    except Exception:
        # Never log request lines, URLs, authentication or provider errors.
        try:
            writer.write(b'HTTP/1.1 502 Bad Gateway\r\nContent-Length: 0\r\n\r\n')
            await writer.drain()
        except ConnectionError:
            pass
    finally:
        if upstream:
            upstream.close()
        writer.close()


async def main():
    SOCKET.parent.mkdir(mode=0o700, parents=True, exist_ok=True)
    if SOCKET.exists():
        raise RuntimeError('proxy_socket_already_exists')
    server = await asyncio.start_unix_server(handle, path=str(SOCKET), limit=16384)
    os.chmod(SOCKET, 0o600)
    print(json.dumps({'proxy_ready': True, 'exact_allowed_hosts': sorted(HOSTS)}), flush=True)
    try:
        async with server:
            await server.serve_forever()
    finally:
        SOCKET.unlink(missing_ok=True)


asyncio.run(main())
