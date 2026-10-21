import { constants, createPrivateKey, createPublicKey, privateDecrypt, publicEncrypt, createCipheriv, createDecipheriv, randomBytes } from 'node:crypto';
import type { KeyObject } from 'node:crypto';
import { Failure, requireThat } from './errors.ts';
import { object } from './model.ts';
import { readPrivate, atomicPrivate } from './private-files.ts';

export const SESSION_CONTEXT = 'buyersworkbench:lenovo:session:v1';
export const OBSERVATION_CONTEXT = 'buyersworkbench:lenovo:observation:v1';
export class EncryptedFiles {
  private readonly privateKey: KeyObject;
  private readonly publicKey: KeyObject;
  constructor(keyFile: string) {
    const bytes = readPrivate(keyFile, 16384);
    try {
      this.privateKey = createPrivateKey(bytes); this.publicKey = createPublicKey(this.privateKey);
      requireThat(this.privateKey.asymmetricKeyType === 'rsa' && (this.privateKey.asymmetricKeyDetails?.modulusLength ?? 0) >= 3072, 'state_invalid');
    } catch { throw new Failure('state_invalid'); }
    finally { bytes.fill(0); }
  }
  read(path: string, context: string, allowProbeImport = false): unknown {
    let key: Buffer | undefined, plain: Buffer | undefined;
    try {
      const envelope = object(JSON.parse(readPrivate(path).toString('utf8')));
      requireThat(envelope.schema === 1 && envelope.algorithm === 'RSA-OAEP-SHA256+A256GCM', 'state_invalid');
      const aad = envelope.context ?? (allowProbeImport ? 'lenovo-vm-probe:v1' : null);
      requireThat(aad === context || (allowProbeImport && aad === 'lenovo-vm-probe:v1'), 'state_invalid');
      const decode = (v: unknown): Buffer => { requireThat(typeof v === 'string' && /^[A-Za-z0-9+/]+={0,2}$/.test(v), 'state_invalid'); return Buffer.from(v, 'base64'); };
      const encrypted = decode(envelope.ciphertext), iv = decode(envelope.iv);
      requireThat(encrypted.length > 16 && iv.length === 12, 'state_invalid');
      key = privateDecrypt({ key: this.privateKey, padding: constants.RSA_PKCS1_OAEP_PADDING, oaepHash: 'sha256' }, decode(envelope.wrappedKey));
      requireThat(key.length === 32, 'state_invalid');
      const decipher = createDecipheriv('aes-256-gcm', key, iv);
      decipher.setAAD(Buffer.from(aad as string)); decipher.setAuthTag(encrypted.subarray(-16));
      plain = Buffer.concat([decipher.update(encrypted.subarray(0, -16)), decipher.final()]);
      return JSON.parse(plain.toString('utf8'));
    } catch { throw new Failure('state_invalid'); }
    finally { key?.fill(0); plain?.fill(0); }
  }
  write(path: string, value: unknown, context: string, exclusive = false): void {
    const key = randomBytes(32), iv = randomBytes(12), plain = Buffer.from(JSON.stringify(value));
    try {
      requireThat(plain.length <= 10 * 1024 * 1024, 'storage_error');
      const cipher = createCipheriv('aes-256-gcm', key, iv); cipher.setAAD(Buffer.from(context));
      const ciphertext = Buffer.concat([cipher.update(plain), cipher.final(), cipher.getAuthTag()]);
      const wrappedKey = publicEncrypt({ key: this.publicKey, padding: constants.RSA_PKCS1_OAEP_PADDING, oaepHash: 'sha256' }, key);
      atomicPrivate(path, JSON.stringify({ schema: 1, context, algorithm: 'RSA-OAEP-SHA256+A256GCM',
        wrappedKey: wrappedKey.toString('base64'), iv: iv.toString('base64'), ciphertext: ciphertext.toString('base64') }), exclusive);
    } catch (error) { throw error instanceof Failure ? error : new Failure('storage_error'); }
    finally { key.fill(0); plain.fill(0); }
  }
}
