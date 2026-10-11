import { createHash, randomUUID } from 'node:crypto';
import { createWriteStream, existsSync } from 'node:fs';
import { mkdir, chmod, rename, rm, readFile } from 'node:fs/promises';
import { dirname, join, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';
import { Readable, Transform } from 'node:stream';
import { pipeline } from 'node:stream/promises';

export const root = fileURLToPath(new URL('../', import.meta.url));
export const nativeName = process.platform === 'win32' ? 'ami-native.exe' : 'ami-native';
export function localBinary(base = root) {
  const local = join(base, 'dist', process.platform === 'win32' ? 'ami.exe' : 'ami');
  return existsSync(local) ? local : join(base, 'bin', nativeName);
}
export async function install({ base = root, releaseBase = 'https://github.com/dogxii/ami/releases/download' } = {}) {
  const target = localBinary(base);
  if (existsSync(target)) return target;
  const { version } = JSON.parse(await readFile(join(base, 'package.json'), 'utf8'));
  if (!/^\d+\.\d+\.\d+$/.test(version)) throw new Error('Invalid native release version');
  const platforms = { darwin: 'darwin', linux: 'linux', win32: 'windows' };
  const arches = { x64: 'amd64', arm64: 'arm64' };
  const platform = platforms[process.platform], arch = arches[process.arch];
  if (!platform || !arch) throw new Error(`Unsupported platform: ${process.platform}/${process.arch}`);
  const asset = `ami_${platform}_${arch}${process.platform === 'win32' ? '.exe' : ''}`;
  const baseURL = `${releaseBase}/v${version}`;
  const signal = AbortSignal.timeout(120000);
  const manifest = await fetch(`${baseURL}/checksums.txt`, { signal });
  if (!manifest.ok) throw new Error(`Native release unavailable (${manifest.status}); download a release or build with Go`);
  const checksum = (await manifest.text()).split(/\r?\n/).map(line => line.trim().split(/\s+/)).find(fields => fields[1]?.replace(/^\*/, '') === asset)?.[0];
  if (!/^[a-f0-9]{64}$/.test(checksum ?? '')) throw new Error('Missing native executable checksum');
  const response = await fetch(`${baseURL}/${asset}`, { signal });
  if (!response.ok || !response.body) throw new Error(`Native download failed (${response.status})`);
  await mkdir(dirname(target), { recursive: true });
  const temporary = `${target}.${randomUUID()}.tmp`;
  const hash = createHash('sha256');
  const hashing = new Transform({ transform(chunk, _encoding, callback) { hash.update(chunk); callback(null, chunk); } });
  try {
    await pipeline(Readable.fromWeb(response.body), hashing, createWriteStream(temporary, { flags: 'wx', mode: 0o755 }));
    if (hash.digest('hex') !== checksum) throw new Error('Native executable checksum mismatch');
    await chmod(temporary, 0o755);
    await rename(temporary, target);
  } finally { await rm(temporary, { force: true }); }
  return target;
}
if (process.argv[1] && resolve(process.argv[1]) === fileURLToPath(import.meta.url) && process.env.AMI_SKIP_DOWNLOAD !== '1') {
  try { await install(); }
  catch (error) { console.error(`ami: ${error.message}`); process.exitCode = 1; }
}
