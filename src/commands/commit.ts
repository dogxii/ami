import { confirm, isCancel } from '@clack/prompts'
import pc from 'picocolors'
import { loadConfig } from '../config/loadConfig'

type CommitFlowOptions = {
  all?: boolean
  yes?: boolean
  pushTarget?: string
}

type CommitFlowResult = 'cancelled' | 'committed' | 'no_changes' | 'no_staged'

export function registerCommitCommand(cli: {
  command: (name: string, description?: string) => any
}) {
  cli
    .command('commit', 'Generate git commit message')
    .option('--all', 'Stage all changes before generating commit message')
    .action(async (options: { all?: boolean }) => {
      try {
        await runCommitFlow({ all: options.all })
      } catch (error) {
        console.log(pc.red('Failed to commit changes'))

        if (error instanceof Error) {
          console.log(error.message)
        }

        process.exitCode = 1
      }
    })
}

export async function runCommitFlow(
  options: CommitFlowOptions,
): Promise<CommitFlowResult> {
  if (options.all) {
    const { stageAllChanges } = await import('../git/stageAllChanges')
    await stageAllChanges()
  }

  const { getGitStatus } = await import('../git/getGitStatus')
  const status = await getGitStatus()

  if (!status) {
    console.log(pc.dim('No changes to commit'))
    return 'no_changes'
  }

  const { getGitDiff } = await import('../git/getGitDiff')
  const diff = await getGitDiff()

  if (!diff) {
    console.log(
      pc.dim('No staged changes. Run git add first, or use ami commit --all.'),
    )
    return 'no_staged'
  }

  const { generateCommitMessage } = await import('../git/commitMessage')
  const config = loadConfig()
  const message = await generateCommitMessage({
    status,
    diff,
    baseUrl: config.baseUrl,
    model: config.model,
    apiKey: config.apiKey,
  })

  if (!message) {
    throw new Error('Empty commit message')
  }

  console.log(pc.bold('Changes'))
  console.log(status)
  console.log('')
  console.log(pc.bold('Commit message'))
  console.log(message)
  console.log('')

  if (!options.yes) {
    const shouldCommit = await confirm({
      message: options.pushTarget
        ? `Commit and push to ${options.pushTarget}?`
        : 'Commit with this message?',
    })

    if (isCancel(shouldCommit) || !shouldCommit) {
      console.log(
        pc.dim(
          options.all
            ? 'Commit canceled. Changes remain staged.'
            : 'Commit canceled',
        ),
      )
      return 'cancelled'
    }
  }

  const { commitChanges } = await import('../git/commitChanges')
  await commitChanges(message)

  const { getLastCommit } = await import('../git/getLastCommit')
  const hash = await getLastCommit()
  console.log(pc.dim(`Committed ${hash}`))

  return 'committed'
}
