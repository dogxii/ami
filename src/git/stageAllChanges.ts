import { $ } from 'bun'

export async function stageAllChanges() {
  const result = await $`git add -A`.quiet().nothrow()

  if (result.exitCode !== 0) {
    throw new Error('Failed to stage git changes')
  }
}
