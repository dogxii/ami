import { $ } from 'bun'

export async function getLastCommit() {
  const result = await $`git rev-parse --short HEAD`.quiet().nothrow()

  if (result.exitCode !== 0) {
    throw new Error('Failed to get last commit')
  }

  return result.stdout.toString().trim()
}
