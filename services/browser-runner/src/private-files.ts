import { constants, openSync, closeSync, fstatSync, readFileSync, lstatSync, mkdirSync, writeFileSync, fsyncSync, renameSync, unlinkSync } from 'node:fs';
import { dirname, join } from 'node:path';
import { randomUUID } from 'node:crypto';
import { Failure, requireThat } from './errors.ts';

export function privateDirectory(path: string): void {
  try {
    try { mkdirSync(path, { mode: 0o700 }); } catch (e) { if ((e as NodeJS.ErrnoException).code !== 'EEXIST') throw e; }
    const s = lstatSync(path);
    requireThat(s.isDirectory() && !s.isSymbolicLink() && (s.mode & 0o077) === 0 && s.uid === process.getuid?.(), 'storage_error');
  } catch { throw new Failure('storage_error'); }
}
export function readPrivate(path: string, maximum = 16 * 1024 * 1024): Buffer {
  let fd: number | undefined;
  try {
    fd = openSync(path, constants.O_RDONLY | constants.O_NOFOLLOW);
    const s = fstatSync(fd);
    requireThat(s.isFile() && s.nlink === 1 && (s.mode & 0o077) === 0 && s.uid === process.getuid?.() && s.size > 0 && s.size <= maximum, 'storage_error');
    return readFileSync(fd);
  } catch { throw new Failure('storage_error'); }
  finally { if (fd !== undefined) closeSync(fd); }
}
export function atomicPrivate(path: string, bytes: string | Buffer, exclusive = false): void {
  const parent = dirname(path); privateDirectory(parent);
  const temporary = join(parent, `.write-${randomUUID()}`);
  let fd: number | undefined;
  try {
    try { const s = lstatSync(path); requireThat(!exclusive && s.isFile() && !s.isSymbolicLink() && (s.mode & 0o077) === 0 && s.uid === process.getuid?.(), 'storage_error'); }
    catch (e) { if ((e as NodeJS.ErrnoException).code !== 'ENOENT') throw e; }
    fd = openSync(temporary, constants.O_WRONLY | constants.O_CREAT | constants.O_EXCL | constants.O_NOFOLLOW, 0o600);
    writeFileSync(fd, bytes); fsyncSync(fd); closeSync(fd); fd = undefined;
    renameSync(temporary, path);
    const directory = openSync(parent, constants.O_RDONLY | constants.O_DIRECTORY);
    try { fsyncSync(directory); } finally { closeSync(directory); }
  } catch { throw new Failure('storage_error'); }
  finally { if (fd !== undefined) closeSync(fd); try { unlinkSync(temporary); } catch {} }
}
