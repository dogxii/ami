import { $ } from 'bun'

export async function getGitStatus() {
  const result = await $`git status --short`.quiet().nothrow()

  if (result.exitCode !== 0) {
    throw new Error('Not a git repository')
  }

  return result.stdout.toString().trim()
}
