import { runCommand } from '../utils/command'

export async function fetchUpstream(upstream: string) {
  const remote = getRemoteName(upstream)
  const result = await runCommand('git', ['fetch', '--quiet', remote])

  if (result.exitCode !== 0) {
    const detail = `${result.stderr}\n${result.stdout}`.trim()
    throw new Error(formatSyncError('fetch', detail))
  }
}

export async function rebaseUpstream(upstream: string) {
  const result = await runCommand('git', ['rebase', upstream])

  return {
    success: result.exitCode === 0,
    detail: `${result.stderr}\n${result.stdout}`.trim(),
  }
}

function getRemoteName(upstream: string) {
  const separator = upstream.indexOf('/')

  if (separator <= 0) {
    throw new Error(`Cannot determine remote from upstream: ${upstream}`)
  }

  return upstream.slice(0, separator)
}

function formatSyncError(action: string, detail: string) {
  if (
    /authentication failed|permission denied|could not read username|access denied|403/i.test(
      detail,
    )
  ) {
    return `Git ${action} authentication failed. Check your remote credentials and access.`
  }

  if (/could not resolve host|unable to access|connection.*failed|timed out/i.test(detail)) {
    return `Unable to ${action} from the Git remote. Check your network and remote URL.`
  }

  return detail || `Failed to ${action} Git upstream.`
}
