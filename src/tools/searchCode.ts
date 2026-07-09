import { opendir, readFile, stat } from 'node:fs/promises'
import { join, relative } from 'node:path'
import { runCommand } from '../utils/command'
import { isBlockedName } from './path'
import type { Tool } from './type'

type SearchCodeInput = {
  query: string
}

const maxOutputChars = 10_000
const maxFileBytes = 1_000_000

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
      '--fixed-strings',
      '--glob',
      '!node_modules/**',
      '--glob',
      '!.git/**',
      '--glob',
      '!**/.env*',
      '--',
      input.query,
      '.',
    ])

    if (result.errorCode === 'ENOENT') {
      return searchWithNode(input.query)
    }

    if (result.exitCode === 1 && !result.stderr) {
      return 'No matches found.'
    }

    if (result.exitCode !== 0) {
      throw new Error(result.stderr.trim() || 'search_code failed')
    }

    return clipOutput(result.stdout)
  },
}

async function searchWithNode(query: string) {
  const output: string[] = []
  let outputLength = 0

  async function visit(directory: string): Promise<void> {
    let entries

    try {
      entries = await opendir(directory)
    } catch {
      return
    }

    for await (const entry of entries) {
      if (outputLength >= maxOutputChars) {
        break
      }

      if (isBlockedName(entry.name)) {
        continue
      }

      const path = join(directory, entry.name)

      if (entry.isDirectory()) {
        await visit(path)
        continue
      }

      if (!entry.isFile()) {
        continue
      }

      try {
        const fileStat = await stat(path)

        if (fileStat.size > maxFileBytes) {
          continue
        }

        const buffer = await readFile(path)

        if (buffer.includes(0)) {
          continue
        }

        const lines = buffer.toString('utf-8').split('\n')

        for (let index = 0; index < lines.length; index++) {
          if (!lines[index].includes(query)) {
            continue
          }

          const match = `${relative(process.cwd(), path)}:${index + 1}:${lines[index]}`
          output.push(match)
          outputLength += match.length + 1

          if (outputLength >= maxOutputChars) {
            break
          }
        }
      } catch {
        continue
      }
    }
  }

  await visit(process.cwd())

  if (output.length === 0) {
    return 'No matches found.'
  }

  return clipOutput(output.join('\n'))
}

function clipOutput(output: string) {
  if (output.length <= maxOutputChars) {
    return output
  }

  return `${output.slice(0, maxOutputChars)}\n...truncated`
}
