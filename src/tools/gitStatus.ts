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
    const output = result.stdout

    if (!output) {
      return 'Working tree clean.'
    }

    return output
  },
}
