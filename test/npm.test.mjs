import { test } from 'node:test';
import assert from 'node:assert/strict';
import { createHash } from 'node:crypto';
import { mkdtemp, writeFile, readFile, rm, mkdir } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { join, resolve } from 'node:path';
import { createServer } from 'node:http';
import { spawn } from 'node:child_process';
import { install, nativeName } from '../bin/install.js';

test('npm and native builds use the same release version', async () => {
  const { version } = JSON.parse(await readFile(new URL('../package.json', import.meta.url), 'utf8'));
  const nativeVersion = (await readFile(new URL('../VERSION', import.meta.url), 'utf8')).trim();
  assert.match(nativeVersion, /^\d+\.\d+\.\d+$/);
  assert.equal(version, nativeVersion);
});

async function fixture(valid = true) {
  const directory = await mkdtemp(join(tmpdir(), 'ami-npm-test-'));
  await writeFile(join(directory, 'package.json'), '{"version":"0.5.0"}');
  const payload = Buffer.from('native executable fixture');
  const os = { darwin: 'darwin', linux: 'linux', win32: 'windows' }[process.platform];
  const arch = { x64: 'amd64', arm64: 'arm64' }[process.arch];
  const asset = `ami_${os}_${arch}${process.platform === 'win32' ? '.exe' : ''}`;
  const hash = valid ? createHash('sha256').update(payload).digest('hex') : '0'.repeat(64);
  const requests = [];
  const server = createServer((req, res) => {
    requests.push(req.url);
    if (req.url.endsWith('/checksums.txt')) res.end(`${hash}  ${asset}\n`);
    else res.end(payload);
  });
  await new Promise(resolve => server.listen(0, '127.0.0.1', resolve));
  return { directory, payload, requests, base: `http://127.0.0.1:${server.address().port}`, cleanup: async () => { server.closeAllConnections(); await new Promise(resolve => server.close(resolve)); await rm(directory, { recursive: true, force: true }); } };
}
test('npm installation downloads and verifies through the configured proxy', async () => {
  const f = await fixture();
  try {
    const script = `import { install } from ${JSON.stringify(new URL('../bin/install.js', import.meta.url).href)}; await install({ base: process.argv[1], releaseBase: process.argv[2] });`;
    const proxyEnv = { ...process.env };
    for (const name of Object.keys(proxyEnv)) if (/^(https?|all|no)_proxy$/i.test(name)) delete proxyEnv[name];
    proxyEnv.http_proxy = f.base;
    const child = spawn(process.execPath, ['--input-type=module', '-e', script, f.directory, 'http://ami-download.invalid'], { env: proxyEnv, stdio: ['ignore', 'ignore', 'pipe'] });
    let error = ''; child.stderr.on('data', chunk => { error += chunk; });
    const code = await new Promise((resolve, reject) => { child.on('error', reject); child.on('close', resolve); });
    assert.equal(code, 0, error);
    assert.deepEqual(await readFile(join(f.directory, 'bin', nativeName)), f.payload);
    assert.equal(f.requests.length, 2);
    assert.ok(f.requests.every(url => url.startsWith('http://ami-download.invalid/')), f.requests);
  } finally { await f.cleanup(); }
});
test('npm installation streams and verifies native executable', async () => {
  const f = await fixture();
  try { const path = await install({ base: f.directory, releaseBase: f.base }); assert.deepEqual(await readFile(path), f.payload); assert.equal(path, join(f.directory, 'bin', nativeName)); }
  finally { await f.cleanup(); }
});
test('npm installation rejects corrupted downloads', async () => {
  const f = await fixture(false);
  try { await assert.rejects(install({ base: f.directory, releaseBase: f.base }), /checksum mismatch/); await assert.rejects(readFile(join(f.directory, 'bin', nativeName)), /ENOENT/); }
  finally { await f.cleanup(); }
});
test('local builds run without downloading a release', async () => {
  const f = await fixture();
  try { await mkdir(join(f.directory, 'dist')); const path = join(f.directory, 'dist', process.platform === 'win32' ? 'ami.exe' : 'ami'); await writeFile(path, 'fixture'); assert.equal(await install({ base: f.directory, releaseBase: 'http://127.0.0.1:1' }), path); }
  finally { await f.cleanup(); }
});
test('direct installer verifies a native release in a temporary destination', { skip: process.platform === 'win32' }, async () => {
  const f = await fixture();
  try {
    const destination = join(f.directory, 'installation with spaces');
    const child = spawn('/bin/sh', [resolve('scripts/install.sh')], { env: { ...process.env, AMI_VERSION: '0.5.0', AMI_INSTALL_DIR: destination, AMI_RELEASE_BASE_URL: f.base }, stdio: ['ignore', 'pipe', 'pipe'] });
    let error = ''; child.stderr.on('data', chunk => { error += chunk; });
    const code = await new Promise((resolve, reject) => { child.on('error', reject); child.on('close', resolve); });
    assert.equal(code, 0, error); assert.deepEqual(await readFile(join(destination, 'ami')), f.payload);
  } finally { await f.cleanup(); }
});
test('direct installer preserves the existing executable when integrity fails', { skip: process.platform === 'win32' }, async () => {
  const f = await fixture(false);
  try {
    const destination = join(f.directory, 'existing'); await mkdir(destination);
    await writeFile(join(destination, 'ami'), 'previous executable');
    const child = spawn('/bin/sh', [resolve('scripts/install.sh')], { env: { ...process.env, AMI_VERSION: '0.5.0', AMI_INSTALL_DIR: destination, AMI_RELEASE_BASE_URL: f.base }, stdio: ['ignore', 'ignore', 'pipe'] });
    let error = ''; child.stderr.on('data', chunk => { error += chunk; });
    const code = await new Promise((resolve, reject) => { child.on('error', reject); child.on('close', resolve); });
    assert.notEqual(code, 0); assert.match(error, /checksum mismatch/);
    assert.equal(await readFile(join(destination, 'ami'), 'utf8'), 'previous executable');
  } finally { await f.cleanup(); }
});
