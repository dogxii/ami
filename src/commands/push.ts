import { confirm, isCancel } from '@clack/prompts'
import pc from 'picocolors'
import { runCommitFlow } from './commit'

export function registerPushCommand(cli: {
  command: (name: string, description?: string) => any
}) {
  cli
    .command('push', 'git push')
    .option('--commit', 'Stage and commit all changes before pushing')
    .option('--yes', 'Push without confirmation')
    .action(async (options: { commit?: boolean; yes?: boolean }) => {
      try {
        const { getCurrentBranch } = await import('../git/getCurrentBranch')
        const { getUpstream } = await import('../git/getUpstream')
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

        const { getConflictState } = await import('../git/conflicts')
        const conflictState = await getConflictState()

        if (conflictState.operation || conflictState.files.length > 0) {
          printConflictState(conflictState)
          process.exitCode = 1
          return
        }

        const { getBranchCounts } = await import('../git/getAheadCount')
        let branchCounts = await getBranchCounts()

        if (branchCounts.behind > 0) {
          console.log(
            pc.red(
              branchCounts.ahead > 0
                ? 'Local and remote branches have diverged.'
                : `Remote branch is ${branchCounts.behind} commit${branchCounts.behind === 1 ? '' : 's'} ahead.`,
            ),
          )
          console.log(
            pc.dim(
              'Fetch the remote changes, then merge or rebase before trying again.',
            ),
          )
          process.exitCode = 1
          return
        }

        const { getGitStatus } = await import('../git/getGitStatus')
        let status = await getGitStatus()
        let pushConfirmed = false

        if (options.commit && status) {
          const commitResult = await runCommitFlow({
            all: true,
            yes: options.yes,
            pushTarget: upstream,
          })

          if (commitResult !== 'committed') {
            return
          }

          pushConfirmed = true
          status = await getGitStatus()
        }

        branchCounts = await getBranchCounts()
        const aheadCount = branchCounts.ahead

        if (aheadCount === 0) {
          if (status) {
            console.log(
              pc.dim(
                'No commits to push. Use ami push --commit to commit local changes first.',
              ),
            )
          } else {
            console.log(pc.dim('No commits to push'))
          }

          return
        }

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

        if (!options.yes && !pushConfirmed) {
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
      }
    })
}

function printConflictState(input: {
  files: string[]
  operation?: string
}) {
  console.log(pc.red('Git operation must be resolved before pushing.'))

  if (input.operation) {
    console.log(`Operation: ${input.operation}`)
  }

  if (input.files.length > 0) {
    console.log('Conflicted files:')

    for (const file of input.files) {
      console.log(`  ${file}`)
    }
  }

  console.log(pc.dim('Resolve the operation, stage the files, then try again.'))
}
