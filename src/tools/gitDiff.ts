import { runCommand } from '../utils/command'
import { truncateToolOutput } from './limits'
import { isBlockedPath, resolveInsideCwd } from './path'
import type { Tool } from './type'

type GitDiffInput = {
  scope?: 'all' | 'staged' | 'working'
  path?: string
}

export const gitDiffTool: Tool<GitDiffInput, string> = {
  name: 'git_diff',
  description:
    'Show tracked Git changes in the current repository. Supports working, staged, or all changes and an optional path. Read-only and excludes blocked paths.',
  parameters: {
    type: 'object',
    properties: {
      scope: {
        type: 'string',
        enum: ['working', 'staged', 'all'],
        description: 'Changes to show. Defaults to working.',
      },
      path: {
        type: 'string',
        description: 'Optional relative file or directory path.',
      },
    },
    additionalProperties: false,
  },
  async run(input) {
    if (typeof input !== 'object' || input === null) {
      throw new Error('git_diff input must be an object')
    }

    const scope = input.scope ?? 'working'

    if (!['working', 'staged', 'all'].includes(scope)) {
      throw new Error('git_diff scope must be working, staged, or all')
    }

    if ('path' in input && typeof input.path !== 'string') {
      throw new Error('git_diff path must be a string')
    }

    const path = getSafePath(input.path)
    const args = ['diff']

    if (scope === 'staged') {
      args.push('--staged')
    } else if (scope === 'all') {
      args.push('HEAD')
    }

    args.push(
      '--',
      path,
      ':(exclude,glob).env*',
      ':(exclude,glob)**/.env*',
    )

    const result = await runCommand('git', args)

    if (result.exitCode !== 0) {
      const detail = result.stderr.trim()

      if (/not a git repository/i.test(detail)) {
        throw new Error('Not a git repository')
      }

      if (scope === 'all' && /unknown revision|bad revision|ambiguous argument/i.test(detail)) {
        throw new Error('git_diff scope all requires at least one commit')
      }

      throw new Error(detail || 'git_diff failed')
    }

    const output = result.stdout.trim()
    return output ? truncateToolOutput(output) : 'No diff found.'
  },
}

function getSafePath(inputPath?: string) {
  if (!inputPath) {
    return '.'
  }

  const { relativePath } = resolveInsideCwd(inputPath)

  if (isBlockedPath(relativePath)) {
    throw new Error('git_diff cannot read blocked path')
  }

  return relativePath || '.'
}
