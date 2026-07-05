import { readFile, stat } from 'node:fs/promises'
import { resolveInsideCwd } from './path'
import type { Tool } from './type'

type ReadFileInput = {
  path: string
}

export const readFileTool: Tool<ReadFileInput, string> = {
  name: 'read_file',
  description:
    'Read a local text file inside current directory. Input JSON: {"path":"relative/file.txt"}. Blocks .env, .git, and node_modules. Max size: 100000 bytes.',
  parameters: {
    type: 'object',
    properties: {
      path: {
        type: 'string',
        description: 'Relative path to the file.',
      },
    },
    required: ['path'],
    additionalProperties: false,
  },
  async run(input) {
    // 输入检查
    if (
      typeof input !== 'object' ||
      input === null ||
      !('path' in input) ||
      typeof input.path !== 'string' ||
      !input.path
    ) {
      throw new Error('read_file requires input: {"path":"file.txt"}')
    }

    // 目录检查
    const { targetPath, relativePath } = resolveInsideCwd(input.path)

    // 文件名检查
    const blockedNames = new Set(['.env', 'node_modules', '.git'])

    if (relativePath.split('/').some((part) => blockedNames.has(part))) {
      throw new Error('read_file cannot read blocked file')
    }

    // 大小检查
    const fileStat = await stat(targetPath)
    const maxBytes = 100_000

    if (fileStat.size > maxBytes) {
      throw new Error(`read_file can only read files up to ${maxBytes} bytes`)
    }

    return await readFile(targetPath, 'utf-8')
  },
}
