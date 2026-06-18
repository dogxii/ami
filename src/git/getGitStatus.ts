import { runCommand } from '../utils/command'

export async function getGitStatus() {
  const result = await runCommand('git', ['status', '--short'])

  if (result.exitCode !== 0) {
    throw new Error('Not a git repository')
  }

  return result.stdout.trim()
}
