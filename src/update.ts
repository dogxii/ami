import { mkdir, readFile, writeFile } from 'node:fs/promises'
import { homedir } from 'node:os'
import { dirname, join } from 'node:path'

export const packageName = '@dogxi/ami'

const registryUrl = 'https://registry.npmjs.org/%40dogxi%2Fami/latest'
const checkInterval = 24 * 60 * 60 * 1000

type UpdateCache = {
  checkedAt: number
  latestVersion: string
}

type LatestVersionOptions = {
  force?: boolean
  timeout?: number
}

export async function findAvailableUpdate(
  currentVersion: string,
): Promise<string | null> {
  if (
    process.env.AMI_NO_UPDATE_CHECK === '1' ||
    !process.stdout.isTTY
  ) {
    return null
  }

  try {
    const latestVersion = await getLatestVersion({ timeout: 1_000 })
    return isNewerVersion(latestVersion, currentVersion)
      ? latestVersion
      : null
  } catch {
    return null
  }
}

export async function getLatestVersion(
  options: LatestVersionOptions = {},
): Promise<string> {
  const cache = await readCache()

  if (
    !options.force &&
    cache &&
    Date.now() - cache.checkedAt < checkInterval
  ) {
    return cache.latestVersion
  }

  try {
    const response = await fetch(registryUrl, {
      headers: {
        accept: 'application/json',
      },
      signal: AbortSignal.timeout(options.timeout ?? 5_000),
    })

    if (!response.ok) {
      throw new Error(`npm registry returned ${response.status}`)
    }

    const data = (await response.json()) as { version?: unknown }

    if (typeof data.version !== 'string' || !parseVersion(data.version)) {
      throw new Error('npm registry returned an invalid version')
    }

    await writeCache({
      checkedAt: Date.now(),
      latestVersion: data.version,
    })

    return data.version
  } catch (error) {
    if (!options.force && cache) {
      return cache.latestVersion
    }

    throw error
  }
}

export function isNewerVersion(candidate: string, current: string) {
  const candidateParts = parseVersion(candidate)
  const currentParts = parseVersion(current)

  if (!candidateParts || !currentParts) {
    return false
  }

  for (let index = 0; index < candidateParts.length; index += 1) {
    if (candidateParts[index] !== currentParts[index]) {
      return candidateParts[index] > currentParts[index]
    }
  }

  return false
}

function parseVersion(value: string) {
  const match = /^(\d+)\.(\d+)\.(\d+)$/.exec(value.trim())

  if (!match) {
    return null
  }

  return match.slice(1).map(Number)
}

async function readCache(): Promise<UpdateCache | null> {
  try {
    const content = await readFile(getCachePath(), 'utf8')
    const cache = JSON.parse(content) as Partial<UpdateCache>

    if (
      typeof cache.checkedAt !== 'number' ||
      typeof cache.latestVersion !== 'string' ||
      !parseVersion(cache.latestVersion)
    ) {
      return null
    }

    return {
      checkedAt: cache.checkedAt,
      latestVersion: cache.latestVersion,
    }
  } catch {
    return null
  }
}

async function writeCache(cache: UpdateCache) {
  const path = getCachePath()

  try {
    await mkdir(dirname(path), { recursive: true, mode: 0o700 })
    await writeFile(path, `${JSON.stringify(cache)}\n`, { mode: 0o600 })
  } catch {
    // A read-only cache directory should not block the CLI.
  }
}

function getCachePath() {
  if (process.platform === 'win32' && process.env.LOCALAPPDATA) {
    return join(process.env.LOCALAPPDATA, 'ami', 'update.json')
  }

  const cacheRoot = process.env.XDG_CACHE_HOME ?? join(homedir(), '.cache')
  return join(cacheRoot, 'ami', 'update.json')
}
