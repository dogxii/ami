import { confirm, isCancel } from '@clack/prompts'
import pc from 'picocolors'
import type { ConflictState } from '../git/conflicts'
import { GitPushError } from '../git/pushChanges'
import { runCommitFlow } from './commit'

type PushOptions = {
  commit?: boolean
  rebase?: boolean
  yes?: boolean
}

type SyncResult = {
  status: 'cancelled' | 'conflict' | 'no_push' | 'ready'
  pushConfirmed: boolean
}

export function registerPushCommand(cli: {
  command: (name: string, description?: string) => any
}) {
  cli
    .command('push', 'git push')
    .option('--commit', 'Stage and commit all changes before pushing')
    .option('--rebase', 'Fetch and rebase local commits before pushing')
    .option(
      '--yes',
      'Skip commit and push confirmations; use with --rebase to approve it',
    )
    .action(async (options: PushOptions) => {
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

        const { getBranchCounts } = await import('../git/getAheadCount')
        let branchCounts = await getBranchCounts()

        if (options.rebase || branchCounts.behind > 0) {
          const syncResult = await syncForPush({
            upstream,
            options,
          })

          if (syncResult.status !== 'ready') {
            if (syncResult.status === 'no_push') {
              console.log(pc.dim('No local commits to push'))
            }

            return
          }

          pushConfirmed ||= syncResult.pushConfirmed
          branchCounts = await getBranchCounts()
        }

        if (branchCounts.ahead === 0) {
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

        printPushSummary({
          branch: currentBranch,
          upstream,
          ahead: branchCounts.ahead,
        })

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

        try {
          await pushChanges()
        } catch (error) {
          if (!(error instanceof GitPushError) || error.kind !== 'non_fast_forward') {
            throw error
          }

          const syncResult = await syncForPush({
            upstream,
            options,
          })

          if (syncResult.status !== 'ready') {
            if (syncResult.status === 'no_push') {
              console.log(pc.dim('No local commits to push'))
            }

            return
          }

          try {
            await pushChanges()
          } catch (retryError) {
            if (
              retryError instanceof GitPushError &&
              retryError.kind === 'non_fast_forward'
            ) {
              throw new Error(
                'Remote changed again during the rebase. Run ami push again.',
              )
            }

            throw retryError
          }
        }

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

async function syncForPush(input: {
  upstream: string
  options: PushOptions
}): Promise<SyncResult> {
  const { fetchUpstream, rebaseUpstream } = await import('../git/sync')
  const { getBranchCounts } = await import('../git/getAheadCount')
  const { getGitStatus } = await import('../git/getGitStatus')

  console.log(pc.dim(`Fetching ${input.upstream}...`))
  await fetchUpstream(input.upstream)

  const branchCounts = await getBranchCounts()

  if (branchCounts.behind === 0) {
    return {
      status: branchCounts.ahead > 0 ? 'ready' : 'no_push',
      pushConfirmed: false,
    }
  }

  if (branchCounts.ahead === 0) {
    console.log(
      pc.dim(
        `Remote branch is ${formatCommitCount(branchCounts.behind)} ahead.`,
      ),
    )

    return {
      status: 'no_push',
      pushConfirmed: false,
    }
  }

  const status = await getGitStatus()

  if (status) {
    console.log(
      pc.red('Cannot rebase while the working tree has uncommitted changes.'),
    )
    console.log(
      pc.dim('Commit or stash them first, or use ami push --commit --rebase.'),
    )
    process.exitCode = 1

    return {
      status: 'cancelled',
      pushConfirmed: false,
    }
  }

  console.log('')
  console.log(pc.bold('Remote changes'))
  console.log(`${formatCommitCount(branchCounts.behind)} ahead remotely`)
  console.log(`${formatCommitCount(branchCounts.ahead)} ahead locally`)

  const skipRebaseConfirmation = Boolean(input.options.rebase && input.options.yes)

  if (!skipRebaseConfirmation) {
    if (!process.stdin.isTTY) {
      console.log(
        pc.dim('Run ami push --rebase --yes to approve the rebase explicitly.'),
      )
      process.exitCode = 1

      return {
        status: 'cancelled',
        pushConfirmed: false,
      }
    }

    const shouldRebase = await confirm({
      message: `Rebase onto ${input.upstream} and continue push?`,
    })

    if (isCancel(shouldRebase) || !shouldRebase) {
      console.log(pc.dim('Rebase canceled'))

      return {
        status: 'cancelled',
        pushConfirmed: false,
      }
    }
  }

  const rebaseResult = await rebaseUpstream(input.upstream)

  if (!rebaseResult.success) {
    const { getConflictState } = await import('../git/conflicts')
    const conflictState = await getConflictState()

    if (conflictState.operation || conflictState.files.length > 0) {
      printRebaseConflict(conflictState)
      process.exitCode = 1

      return {
        status: 'conflict',
        pushConfirmed: false,
      }
    }

    throw new Error(rebaseResult.detail || 'Failed to rebase local commits.')
  }

  console.log(pc.dim(`Rebased onto ${input.upstream}`))

  const updatedCounts = await getBranchCounts()

  return {
    status: updatedCounts.ahead > 0 ? 'ready' : 'no_push',
    pushConfirmed: !skipRebaseConfirmation,
  }
}

function printPushSummary(input: {
  branch: string
  upstream: string
  ahead: number
}) {
  console.log(pc.bold('Branch:'))
  console.log(input.branch)
  console.log(pc.bold('Upstream:'))
  console.log(input.upstream)
  console.log(pc.bold('Commits:'))
  console.log(`${input.ahead} ahead`)
}

function printConflictState(input: ConflictState) {
  console.log(pc.red('Git operation must be resolved before pushing.'))

  if (input.operation) {
    console.log(`Operation: ${input.operation}`)
  }

  printConflictFiles(input.files)
  console.log(pc.dim('Resolve the operation, stage the files, then try again.'))
}

function printRebaseConflict(input: ConflictState) {
  console.log(pc.red('Rebase stopped because of conflicts.'))
  printConflictFiles(input.files)
  console.log('')
  console.log(pc.dim('After resolving the files:'))
  console.log(pc.dim('  git add <files>'))
  console.log(pc.dim('  git rebase --continue'))
  console.log(pc.dim('Abort with:'))
  console.log(pc.dim('  git rebase --abort'))
  console.log(pc.dim('Ask Ami for help:'))
  console.log(pc.dim('  ami "explain the current rebase conflicts"'))
}

function printConflictFiles(files: string[]) {
  if (files.length === 0) {
    return
  }

  console.log('Conflicted files:')

  for (const file of files) {
    console.log(`  ${file}`)
  }
}

function formatCommitCount(count: number) {
  return `${count} commit${count === 1 ? '' : 's'}`
}
