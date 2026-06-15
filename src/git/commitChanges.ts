import { $ } from 'bun'

export async function commitChanges(message: string) {
  const result = await $`git commit -m ${message}`.quiet().nothrow()

  if (result.exitCode !== 0) {
    throw new Error('Failed to commit changes')
  }
}
