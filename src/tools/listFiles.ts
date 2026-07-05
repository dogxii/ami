import { readdir } from 'node:fs/promises'
import { resolveInsideCwd } from './path'
import type { Tool } from './type'

type ListFilesInput = {
  path?: string
}

export const listFilesTool: Tool<ListFilesInput, string> = {
  name: 'list_files',
  description:
    'List files in a directory inside the current directory. Input JSON: {"path":"relative/dir"} or {}.',
  parameters: {
    type: 'object',
    properties: {
      path: {
        type: 'string',
        description: 'Relative directory path. Defaults to the current directory.',
      },
    },
    additionalProperties: false,
  },
  async run(input) {
    if (typeof input !== 'object' || input === null) {
      throw new Error(
        'list_files requires input: {} or {"path":"relative/dir"}',
      )
    }

    if ('path' in input && typeof input.path !== 'string') {
      throw new Error('list_files path must be a string')
    }

    const dirPath = input.path ?? '.'

    // 目录检查
    const { targetPath } = resolveInsideCwd(dirPath)

    const entries = await readdir(targetPath, { withFileTypes: true })

    return entries
      .map((entry) => (entry.isDirectory() ? `${entry.name}/` : entry.name))
      .join('\n')
  },
}
