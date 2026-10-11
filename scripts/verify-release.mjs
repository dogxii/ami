import assert from 'node:assert/strict';
import { execFileSync } from 'node:child_process';
import { createHash } from 'node:crypto';
import { mkdtemp, readFile, writeFile, rm, mkdir } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { join, resolve } from 'node:path';
import { install, nativeName } from '../bin/install.js';

// Verify the package people install, using the real, versioned GitHub release.
const { version } = JSON.parse(await readFile('package.json', 'utf8'));
const directory = await mkdtemp(join(tmpdir(), 'ami-release-check-'));
const environment = { ...process.env, AMI_STATE_DIR: join(directory, 'state') };
delete environment.AMI_SKIP_DOWNLOAD;
const run = (program, args, cwd = directory, env = environment) => execFileSync(program, args, { cwd, env, encoding: 'utf8', timeout: 180000 });
// npm run supplies its JS entry on every platform, avoiding Windows cmd quoting.
const npmCLI = process.env.npm_execpath;
assert.ok(npmCLI, 'Run this check with npm run verify:release');
const npm = (args, cwd = process.cwd()) => run(process.execPath, [npmCLI, ...args], cwd);
try {
  const packed = JSON.parse(npm(['pack', '--json', '--pack-destination', directory]))[0];
  assert.equal(packed.version, version);
  assert.equal(packed.files.some(file => file.path.includes('ami-native') || file.path.startsWith('dist/')), false);
  const project = join(directory, 'project');
  await mkdir(project);
  await writeFile(join(project, 'package.json'), JSON.stringify({ name: 'ami-release-check', private: true }));
  npm(['install', '--no-audit', '--no-fund', '--package-lock=false', join(directory, packed.filename)], project);
  const base = join(project, 'node_modules', '@dogxi', 'ami');
  const native = join(base, 'bin', nativeName);
  assert.equal(run(native, ['--version'], project).trim(), `ami/${version}`);
  assert.equal(run(process.execPath, [join(base, 'bin', 'ami.js'), '--version'], project).trim(), `ami/${version}`);
  const command = process.platform === 'win32' ? 'Write-Output release-ready' : 'printf release-ready';
  const result = JSON.parse(run(native, ['tool', 'exec_command', JSON.stringify({ command, waitMs: 30000 })], project));
  assert.equal(result.exitCode, 0);
  assert.equal(result.stdout.content.trim(), 'release-ready');
  const direct = join(directory, 'direct');
  await mkdir(direct);
  if (process.platform !== 'win32') {
    run('/bin/sh', [resolve('scripts/install.sh')], direct, { ...environment, AMI_INSTALL_DIR: direct, AMI_VERSION: version });
    assert.equal(run(join(direct, 'ami'), ['--version'], direct).trim(), `ami/${version}`);
    assert.equal(createHash('sha256').update(await readFile(join(direct, 'ami'))).digest('hex'), createHash('sha256').update(await readFile(native)).digest('hex'));
  } else {
    await writeFile(join(direct, 'package.json'), JSON.stringify({ version }));
    const standalone = await install({ base: direct });
    assert.equal(run(standalone, ['--version'], direct).trim(), `ami/${version}`);
  }
  console.log(`Verified ami/${version}: packed npm install, native version, real command and direct installation`);
} finally {
  await rm(directory, { recursive: true, force: true });
}
