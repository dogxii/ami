import { runCommand } from '../utils/command'

export async function getAheadCount() {
  const result = await runCommand('git', [
    'rev-list',
    '--count',
    '@{u}..HEAD',
  ])

  if (result.exitCode !== 0) {
    throw new Error('Failed to get ahead count')
  }

  return Number(result.stdout.trim())
}
