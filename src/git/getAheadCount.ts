import { $ } from 'bun'

export async function getAheadCount() {
  const result = await $`git rev-list --count @{u}..HEAD`.quiet().nothrow()

  if (result.exitCode !== 0) {
    throw new Error('Failed to get ahead count')
  }

  return Number(result.stdout.toString().trim())
}
