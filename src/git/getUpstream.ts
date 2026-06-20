import { runCommand } from '../utils/command'

export async function getUpstream() {
  const result = await runCommand('git', [
    'rev-parse',
    '--abbrev-ref',
    '--symbolic-full-name',
    '@{u}',
  ])

  if (result.exitCode !== 0) {
    return ''
  }

  return result.stdout.trim()
}
