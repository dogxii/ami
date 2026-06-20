import { runCommand } from '../utils/command'

export async function commitChanges(message: string) {
  const result = await runCommand('git', ['commit', '-m', message])

  if (result.exitCode !== 0) {
    throw new Error('Failed to commit changes')
  }
}
