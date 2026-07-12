import { runCommand } from '../utils/command'

export class GitPushError extends Error {
  constructor(
    message: string,
    readonly kind: 'auth' | 'network' | 'non_fast_forward' | 'remote' | 'unknown',
  ) {
    super(message)
    this.name = 'GitPushError'
  }
}

export async function pushChanges() {
  const result = await runCommand('git', ['push'])

  if (result.exitCode === 0) {
    return
  }

  const detail = `${result.stderr}\n${result.stdout}`.trim()

  if (/non-fast-forward|fetch first|rejected.*HEAD/i.test(detail)) {
    throw new GitPushError(
      'Push rejected because the remote branch has new commits. Fetch the remote changes, then merge or rebase before trying again.',
      'non_fast_forward',
    )
  }

  if (
    /authentication failed|permission denied|could not read username|access denied|403/i.test(
      detail,
    )
  ) {
    throw new GitPushError(
      'Git authentication failed. Check your remote credentials and access.',
      'auth',
    )
  }

  if (/could not resolve host|unable to access|connection.*failed|timed out/i.test(detail)) {
    throw new GitPushError(
      'Unable to reach the Git remote. Check your network and remote URL.',
      'network',
    )
  }

  if (/repository not found|does not appear to be a git repository/i.test(detail)) {
    throw new GitPushError(
      'Git remote repository was not found. Check the remote URL and access.',
      'remote',
    )
  }

  throw new GitPushError(detail || 'Failed to push changes.', 'unknown')
}
