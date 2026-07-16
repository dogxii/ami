import { readFile, stat } from 'node:fs/promises'
import { maxToolOutputChars } from './limits'
import { isBlockedPath, resolveInsideCwd } from './path'
import type { Tool } from './type'

type ReadFileInput = {
  path: string
  startLine?: number
  endLine?: number
}

const maxFullFileBytes = 100_000
const maxRangedFileBytes = 1_000_000
const maxRangeLines = 500

export const readFileTool: Tool<ReadFileInput, string> = {
  name: 'read_file',
  description:
    'Read a text file inside the current directory. Optional line ranges reduce context for large files. Blocks .env*, .git, and node_modules.',
  parameters: {
    type: 'object',
    properties: {
      path: {
        type: 'string',
        description: 'Relative path to the file.',
      },
      startLine: {
        type: 'integer',
        minimum: 1,
        description: 'First line to read, starting at 1.',
      },
      endLine: {
        type: 'integer',
        minimum: 1,
        description: 'Last line to read, inclusive.',
      },
    },
    required: ['path'],
    additionalProperties: false,
  },
  async run(input) {
    validateInput(input)

    const { targetPath, relativePath } = resolveInsideCwd(input.path)

    if (isBlockedPath(relativePath)) {
      throw new Error('read_file cannot read blocked file')
    }

    const fileStat = await stat(targetPath)

    if (!fileStat.isFile()) {
      throw new Error('read_file path must be a file')
    }

    const hasRange = input.startLine !== undefined || input.endLine !== undefined
    const maxBytes = hasRange ? maxRangedFileBytes : maxFullFileBytes

    if (fileStat.size > maxBytes) {
      throw new Error(
        hasRange
          ? `read_file can read ranged files up to ${maxRangedFileBytes} bytes`
          : `File is larger than ${maxFullFileBytes} bytes. Use startLine and endLine to read a range.`,
      )
    }

    const buffer = await readFile(targetPath)

    if (buffer.includes(0)) {
      throw new Error('read_file only supports text files')
    }

    const content = buffer.toString('utf-8')

    if (!hasRange) {
      return formatFullFile(content)
    }

    return formatRange(content, input.startLine, input.endLine)
  },
}

function validateInput(input: unknown): asserts input is ReadFileInput {
  if (
    typeof input !== 'object' ||
    input === null ||
    !('path' in input) ||
    typeof input.path !== 'string' ||
    !input.path
  ) {
    throw new Error('read_file requires a file path')
  }

  const values = input as Record<string, unknown>

  for (const key of ['startLine', 'endLine'] as const) {
    const value = values[key]

    if (
      key in values &&
      (typeof value !== 'number' ||
        !Number.isInteger(value) ||
        value < 1)
    ) {
      throw new Error(`read_file ${key} must be a positive integer`)
    }
  }

  if (
    'startLine' in input &&
    'endLine' in input &&
    typeof input.startLine === 'number' &&
    typeof input.endLine === 'number' &&
    input.endLine < input.startLine
  ) {
    throw new Error('read_file endLine must be greater than startLine')
  }
}

function formatFullFile(content: string) {
  if (content.length <= maxToolOutputChars) {
    return content
  }

  const lineCount = content.split('\n').length

  return [
    content.slice(0, maxToolOutputChars),
    '',
    `...truncated (${lineCount} lines total). Use startLine and endLine to read more.`,
  ].join('\n')
}

function formatRange(content: string, startInput?: number, endInput?: number) {
  const lines = content.split('\n')
  const startLine = startInput ?? 1
  const endLine = endInput ?? Math.min(lines.length, startLine + maxRangeLines - 1)

  if (startLine > lines.length) {
    throw new Error(`read_file startLine exceeds file length (${lines.length})`)
  }

  if (endLine - startLine + 1 > maxRangeLines) {
    throw new Error(`read_file can return at most ${maxRangeLines} lines`)
  }

  const selected = lines
    .slice(startLine - 1, Math.min(endLine, lines.length))
    .map((line, index) => `${startLine + index}: ${line}`)
    .join('\n')

  if (selected.length <= maxToolOutputChars) {
    return selected
  }

  return `${selected.slice(0, maxToolOutputChars)}\n\n...truncated by output limit`
}
