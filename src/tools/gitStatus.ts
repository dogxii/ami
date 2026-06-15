import { $ } from 'bun'
import type { Tool } from './type'

export const gitStatusTool: Tool<Record<string, never>, string> = {
  name: 'git_status',
  description: 'Show git working tree. Input JSON: {}.',
  async run() {
    const result = await $`git status --short`.quiet().nothrow()
    const output = result.stdout.toString()

    if (!output) {
      return 'Working tree clean.'
    }

    return output
  },
}
