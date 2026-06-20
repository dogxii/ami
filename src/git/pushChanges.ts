import { runCommand } from '../utils/command'

export async function pushChanges() {
  const result = await runCommand('git', ['push'])

  if (result.exitCode !== 0) {
    throw new Error('Failed to push changes')
  }
}
