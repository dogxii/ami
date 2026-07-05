import { runCommand } from '../utils/command'
import type { Tool } from './type'

type SearchCodeInput = {
  query: string
}

export const searchCodeTool: Tool<SearchCodeInput, string> = {
  name: 'search_code',
  description:
    'Search code text inside the current directory. Input JSON: {"query":"text"}.',
  parameters: {
    type: 'object',
    properties: {
      query: {
        type: 'string',
        description: 'Text to search for.',
      },
    },
    required: ['query'],
    additionalProperties: false,
  },
  async run(input) {
    if (
      typeof input !== 'object' ||
      input === null ||
      !('query' in input) ||
      typeof input.query !== 'string' ||
      !input.query
    ) {
      throw new Error('search_code requires input: {"query": "text"}')
    }

    const result = await runCommand('rg', [
      '--line-number',
      '--hidden',
      '--glob',
      '!node_modules/**',
      '--glob',
      '!.git/**',
      '--glob',
      '!.env',
      '--',
      input.query,
      '.',
    ])

    const output = result.stdout

    if (!output) {
      return 'No matches found.'
    }

    const maxChars = 10_000
    if (output.length > maxChars) {
      return `${output.slice(0, maxChars)}\n...truncated`
    }
    return output
  },
}
