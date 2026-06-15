import { $ } from 'bun'

export async function pushChanges() {
  const result = await $`git push`.quiet().nothrow()

  if (result.exitCode !== 0) {
    throw new Error('Failed to push changes')
  }
}
