import { access } from 'node:fs/promises'
import { runCommand } from '../utils/command'

export type ConflictState = {
  files: string[]
  operation?: 'cherry-pick' | 'merge' | 'rebase' | 'revert'
}

export async function getConflictState(): Promise<ConflictState> {
  const filesResult = await runCommand('git', [
    'diff',
    '--name-only',
    '--diff-filter=U',
  ])

  if (filesResult.exitCode !== 0) {
    throw new Error('Not a git repository')
  }

  const operations = [
    ['merge', 'MERGE_HEAD'],
    ['rebase', 'rebase-merge'],
    ['rebase', 'rebase-apply'],
    ['cherry-pick', 'CHERRY_PICK_HEAD'],
    ['revert', 'REVERT_HEAD'],
  ] as const

  let operation: ConflictState['operation']

  for (const [name, gitPath] of operations) {
    const pathResult = await runCommand('git', ['rev-parse', '--git-path', gitPath])

    if (pathResult.exitCode === 0 && (await pathExists(pathResult.stdout.trim()))) {
      operation = name
      break
    }
  }

  return {
    files: filesResult.stdout.split('\n').filter(Boolean),
    operation,
  }
}

function pathExists(path: string) {
  return access(path).then(
    () => true,
    () => false,
  )
}
