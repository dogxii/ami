import { $ } from 'bun'

export async function getGitDiff() {
  const result = await $`git diff --staged`.quiet().nothrow()

  if (result.exitCode !== 0) {
    throw new Error('Failed to read git diff')
  }

  return result.stdout.toString().trim()
}
