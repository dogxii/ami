import { $ } from 'bun'

export async function getUpstream() {
  const result = await $`git rev-parse --abbrev-ref --symbolic-full-name @{u}`
    .quiet()
    .nothrow()

  if (result.exitCode !== 0) {
    return ''
  }

  return result.stdout.toString().trim()
}
