import { runCommand } from '../utils/command'

export async function getCurrentBranch() {
  const result = await runCommand('git', ['branch', '--show-current'])

  if (result.exitCode !== 0) {
    throw new Error('Failed to get current branch')
  }

  return result.stdout.trim()
}
