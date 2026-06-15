import { confirm, isCancel } from '@clack/prompts'
import pc from 'picocolors'
import { loadConfig } from '../config/loadConfig'

export function registerCommitCommand(cli: {
  command: (name: string, description?: string) => any
}) {
  cli
    .command('commit', 'Generate git commit message')
    .option('--all', 'Stage all changes before generating commit message')
    .action(async (options: { all?: boolean }) => {
      const { getGitStatus } = await import('../git/getGitStatus')

      try {
        if (options.all) {
          const { stageAllChanges } = await import('../git/stageAllChanges')
          await stageAllChanges()
        }

        const status = await getGitStatus()

        if (!status) {
          console.log(pc.dim('No changes to commit'))
          return
        }

        const { getGitDiff } = await import('../git/getGitDiff')
        const diff = await getGitDiff()

        if (!diff) {
          console.log(
            pc.dim(
              'No staged changes. Run git add first, or use ami commit --all.',
            ),
          )
          return
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

        const shouldCommit = await confirm({
          message: 'Commit with this message?',
        })

        if (isCancel(shouldCommit) || !shouldCommit) {
          console.log(pc.dim('Commit canceled'))
          return
        }

        const { commitChanges } = await import('../git/commitChanges')
        await commitChanges(message)

        const { getLastCommit } = await import('../git/getLastCommit')
        const hash = await getLastCommit()

        console.log(pc.dim(`Committed ${hash}`))
      } catch (error) {
        console.log(pc.red('Failed to commit changes'))

        if (error instanceof Error) {
          console.log(error.message)
        }

        process.exitCode = 1
      }
    })
}
