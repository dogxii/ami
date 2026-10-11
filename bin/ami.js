#!/usr/bin/env node
import { spawn } from 'node:child_process';
import { install } from './install.js';
try {
  const binary = await install();
  const child = spawn(binary, process.argv.slice(2), { stdio: 'inherit' });
  for (const signal of ['SIGINT', 'SIGTERM']) process.on(signal, () => child.kill(signal));
  child.on('error', error => { console.error(`ami: ${error.message}`); process.exitCode = 1; });
  child.on('exit', (code, signal) => { process.exitCode = code ?? (signal === 'SIGINT' ? 130 : 1); });
} catch (error) { console.error(`ami: ${error.message}`); process.exitCode = 1; }
