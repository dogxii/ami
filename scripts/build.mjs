import { execFileSync } from 'node:child_process';
const output = process.platform === 'win32' ? 'dist/ami.exe' : 'dist/ami';
execFileSync('go', ['build', '-trimpath', '-ldflags=-s -w', '-o', output, './cmd/ami'], { stdio: 'inherit', env: { ...process.env, CGO_ENABLED: '0' } });
