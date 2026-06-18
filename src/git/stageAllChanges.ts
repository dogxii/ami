import { runCommand } from '../utils/command'

export async function stageAllChanges() {
  const result = await runCommand('git', ['add', '-A'])

  if (result.exitCode !== 0) {
    throw new Error('Failed to stage git changes')
  }
}
