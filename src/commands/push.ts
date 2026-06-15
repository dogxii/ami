import { confirm, isCancel } from '@clack/prompts'
import pc from 'picocolors'

export function registerPushCommand(cli: {
  command: (name: string, description?: string) => any
}) {
  cli
    .command('push', 'git push')
    .option('--yes', 'Push without confirmation')
    .action(async (options: { yes?: boolean }) => {
      const { getCurrentBranch } = await import('../git/getCurrentBranch')
      const { getUpstream } = await import('../git/getUpstream')

      try {
        const currentBranch = await getCurrentBranch()
        const upstream = await getUpstream()

        if (!upstream) {
          console.log(
            pc.dim(
              `No upstream branch. Use git push -u origin ${currentBranch} first.`,
            ),
          )
          return
        }

        const { getAheadCount } = await import('../git/getAheadCount')
        const aheadCount = await getAheadCount()

        if (aheadCount === 0) {
          console.log(pc.dim('No commits to push'))
          return
        }

        const { getGitStatus } = await import('../git/getGitStatus')
        const status = await getGitStatus()

        if (status) {
          console.log(
            pc.dim(
              'You have uncommitted changes. Only committed changes will be pushed.',
            ),
          )
          console.log('')
        }

        console.log(pc.bold('Branch:'))
        console.log(currentBranch)
        console.log(pc.bold('Upstream:'))
        console.log(upstream)
        console.log(pc.bold('Commits:'))
        console.log(`${aheadCount} ahead`)

        if (!options.yes) {
          const shouldPush = await confirm({
            message: `Push to ${upstream}?`,
          })

          if (isCancel(shouldPush) || !shouldPush) {
            console.log(pc.dim('Push canceled'))
            return
          }
        }

        const { pushChanges } = await import('../git/pushChanges')
        await pushChanges()

        console.log(pc.dim('Pushed'))
      } catch (error) {
        console.log(pc.red('Failed to push changes'))

        if (error instanceof Error) {
          console.log(error.message)
        }

        process.exitCode = 1
        return
      }
    })
}
