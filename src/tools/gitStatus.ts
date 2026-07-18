import { runCommand } from '../utils/command'
import type { Tool } from './type'

export const gitStatusTool: Tool<Record<string, never>, string> = {
  name: 'git_status',
  description: 'Show the current Git working tree status. Read-only.',
  parameters: {
    type: 'object',
    additionalProperties: false,
  },
  async run() {
    const result = await runCommand('git', ['status', '--short'])

    if (result.exitCode !== 0) {
      throw new Error('Not a git repository')
    }

    const output = result.stdout

    if (!output) {
      return 'Working tree clean.'
    }

    return output
  },
}
