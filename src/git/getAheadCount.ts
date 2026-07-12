import { runCommand } from '../utils/command'

export async function getAheadCount() {
  return (await getBranchCounts()).ahead
}

export async function getBranchCounts() {
  const result = await runCommand('git', [
    'rev-list',
    '--left-right',
    '--count',
    'HEAD...@{u}',
  ])

  if (result.exitCode !== 0) {
    throw new Error('Failed to compare local and upstream branches')
  }

  const [ahead = '0', behind = '0'] = result.stdout.trim().split(/\s+/)

  return {
    ahead: Number(ahead),
    behind: Number(behind),
  }
}
