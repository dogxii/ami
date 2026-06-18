import { runCommand } from '../utils/command'

export async function getGitDiff() {
  const result = await runCommand('git', ['diff', '--staged'])

  if (result.exitCode !== 0) {
    throw new Error('Failed to read git diff')
  }

  return result.stdout.trim()
}
