import { readdir } from 'node:fs/promises'
import { join, relative } from 'node:path'
import { isBlockedName, isBlockedPath, resolveInsideCwd } from './path'
import type { Tool } from './type'

type ListFilesInput = {
  path?: string
  depth?: number
}

const maxDepth = 3
const maxEntries = 300

export const listFilesTool: Tool<ListFilesInput, string> = {
  name: 'list_files',
  description:
    'List files and directories inside the current directory. Supports up to 3 levels. Shows blocked entries but never reads inside them.',
  parameters: {
    type: 'object',
    properties: {
      path: {
        type: 'string',
        description: 'Relative directory path. Defaults to the current directory.',
      },
      depth: {
        type: 'integer',
        minimum: 1,
        maximum: maxDepth,
        description: 'Directory levels to list. Defaults to 1.',
      },
    },
    additionalProperties: false,
  },
  async run(input) {
    validateInput(input)

    const dirPath = input.path ?? '.'
    const depth = input.depth ?? 1
    const { targetPath, relativePath } = resolveInsideCwd(dirPath)

    if (isBlockedPath(relativePath)) {
      throw new Error('list_files cannot enter blocked directory')
    }

    const output: string[] = []
    let truncated = false

    async function visit(directory: string, currentDepth: number) {
      if (output.length >= maxEntries) {
        truncated = true
        return
      }

      const entries = await readdir(directory, { withFileTypes: true })
      entries.sort((left, right) => left.name.localeCompare(right.name))

      for (const entry of entries) {
        if (output.length >= maxEntries) {
          truncated = true
          return
        }

        const path = join(directory, entry.name)
        const displayPath = relative(process.cwd(), path) || entry.name
        const blocked = isBlockedName(entry.name)
        const suffix = entry.isDirectory() ? '/' : ''
        output.push(`${displayPath}${suffix}${blocked ? ' [blocked]' : ''}`)

        if (
          entry.isDirectory() &&
          !blocked &&
          currentDepth < depth
        ) {
          await visit(path, currentDepth + 1)
        }
      }
    }

    await visit(targetPath, 1)

    if (output.length === 0) {
      return 'Directory is empty.'
    }

    if (truncated) {
      output.push(`...truncated (${maxEntries} entries shown)`)
    }

    return output.join('\n')
  },
}

function validateInput(input: unknown): asserts input is ListFilesInput {
  if (typeof input !== 'object' || input === null) {
    throw new Error('list_files input must be an object')
  }

  if ('path' in input && typeof input.path !== 'string') {
    throw new Error('list_files path must be a string')
  }

  if (
    'depth' in input &&
    (typeof input.depth !== 'number' ||
      !Number.isInteger(input.depth) ||
      input.depth < 1 ||
      input.depth > maxDepth)
  ) {
    throw new Error(`list_files depth must be between 1 and ${maxDepth}`)
  }
}
