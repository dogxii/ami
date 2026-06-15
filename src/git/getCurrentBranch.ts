import { $ } from 'bun'

export async function getCurrentBranch() {
  const result = await $`git branch --show-current`.quiet().nothrow()

  if (result.exitCode !== 0) {
    throw new Error('Failed to get current branch')
  }

  return result.stdout.toString().trim()
}
