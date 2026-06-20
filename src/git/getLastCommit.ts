import { runCommand } from '../utils/command'

export async function getLastCommit() {
  const result = await runCommand('git', ['rev-parse', '--short', 'HEAD'])

  if (result.exitCode !== 0) {
    throw new Error('Failed to get last commit')
  }

  return result.stdout.trim()
}
