import { readFile, readdir, stat } from 'node:fs/promises'
import { join, relative } from 'node:path'
import { runCommand } from '../utils/command'
import { truncateToolOutput } from './limits'
import { isBlockedName, isBlockedPath, resolveInsideCwd } from './path'
import type { Tool } from './type'

type SearchCodeInput = {
  query: string
  path?: string
  contextLines?: number
  maxResults?: number
}

type SearchEvent = {
  path: string
  lineNumber: number
  content: string
  match: boolean
}

type RgEvent = {
  type?: 'context' | 'match'
  data?: {
    path?: { text?: string }
    lines?: { text?: string }
    line_number?: number
  }
}

const maxFileBytes = 1_000_000
const maxContextLines = 3
const maxSearchResults = 50
const defaultSearchResults = 20

export const searchCodeTool: Tool<SearchCodeInput, string> = {
  name: 'search_code',
  description:
    'Search literal text inside the current directory. Supports a path, nearby context lines, and a result limit. Excludes blocked paths.',
  parameters: {
    type: 'object',
    properties: {
      query: {
        type: 'string',
        description: 'Literal text to search for.',
      },
      path: {
        type: 'string',
        description: 'Relative file or directory path. Defaults to current directory.',
      },
      contextLines: {
        type: 'integer',
        minimum: 0,
        maximum: maxContextLines,
        description: 'Nearby lines to include around each match. Defaults to 0.',
      },
      maxResults: {
        type: 'integer',
        minimum: 1,
        maximum: maxSearchResults,
        description: `Maximum matching lines. Defaults to ${defaultSearchResults}.`,
      },
    },
    required: ['query'],
    additionalProperties: false,
  },
  async run(input) {
    validateInput(input)

    const contextLines = input.contextLines ?? 0
    const maxResults = input.maxResults ?? defaultSearchResults
    const { targetPath, relativePath } = resolveInsideCwd(input.path ?? '.')

    if (isBlockedPath(relativePath)) {
      throw new Error('search_code cannot search blocked path')
    }

    const searchPath = relativePath || '.'
    const result = await runCommand('rg', [
      '--json',
      '--hidden',
      '--fixed-strings',
      '--context',
      String(contextLines),
      '--max-count',
      String(maxResults),
      '--glob',
      '!**/node_modules/**',
      '--glob',
      '!**/.git/**',
      '--glob',
      '!**/.env*',
      '--',
      input.query,
      searchPath,
    ])

    if (result.errorCode === 'ENOENT') {
      return searchWithNode({
        query: input.query,
        targetPath,
        contextLines,
        maxResults,
      })
    }

    if (result.exitCode === 1 && !result.stderr) {
      return 'No matches found.'
    }

    if (result.exitCode !== 0) {
      throw new Error(result.stderr.trim() || 'search_code failed')
    }

    return formatRgOutput(result.stdout, contextLines, maxResults)
  },
}

function validateInput(input: unknown): asserts input is SearchCodeInput {
  if (
    typeof input !== 'object' ||
    input === null ||
    !('query' in input) ||
    typeof input.query !== 'string' ||
    !input.query.trim()
  ) {
    throw new Error('search_code requires a non-empty query')
  }

  const values = input as Record<string, unknown>

  if ('path' in values && typeof values.path !== 'string') {
    throw new Error('search_code path must be a string')
  }

  validateInteger(values, 'contextLines', 0, maxContextLines)
  validateInteger(values, 'maxResults', 1, maxSearchResults)
}

function validateInteger(
  values: Record<string, unknown>,
  key: string,
  minimum: number,
  maximum: number,
) {
  if (!(key in values)) {
    return
  }

  const value = values[key]

  if (
    typeof value !== 'number' ||
    !Number.isInteger(value) ||
    value < minimum ||
    value > maximum
  ) {
    throw new Error(`search_code ${key} must be between ${minimum} and ${maximum}`)
  }
}

function formatRgOutput(
  output: string,
  contextLines: number,
  maxResults: number,
) {
  const events: SearchEvent[] = []

  for (const line of output.split('\n')) {
    if (!line) {
      continue
    }

    let event: RgEvent

    try {
      event = JSON.parse(line) as RgEvent
    } catch {
      continue
    }

    if (event.type !== 'match' && event.type !== 'context') {
      continue
    }

    const path = event.data?.path?.text
    const lineNumber = event.data?.line_number
    const content = event.data?.lines?.text

    if (!path || lineNumber === undefined || content === undefined) {
      continue
    }

    events.push({
      path,
      lineNumber,
      content: content.replace(/\r?\n$/, ''),
      match: event.type === 'match',
    })
  }

  const matches = events.filter((event) => event.match)
  const selectedMatches = matches.slice(0, maxResults)

  if (selectedMatches.length === 0) {
    return 'No matches found.'
  }

  const selectedKeys = new Set(
    selectedMatches.map((event) => `${event.path}:${event.lineNumber}`),
  )
  const visibleEvents = events.filter((event) =>
    selectedMatches.some(
      (match) =>
        match.path === event.path &&
        Math.abs(match.lineNumber - event.lineNumber) <= contextLines,
    ),
  )

  return formatSearchEvents({
    events: visibleEvents,
    selectedKeys,
    limited: matches.length > selectedMatches.length,
    shownMatches: selectedMatches.length,
  })
}

async function searchWithNode(input: {
  query: string
  targetPath: string
  contextLines: number
  maxResults: number
}) {
  const eventMap = new Map<string, SearchEvent>()
  const targetStat = await stat(input.targetPath)
  let matchCount = 0
  let limited = false

  async function searchFile(path: string) {
    if (matchCount >= input.maxResults) {
      limited = true
      return
    }

    try {
      const fileStat = await stat(path)

      if (!fileStat.isFile() || fileStat.size > maxFileBytes) {
        return
      }

      const buffer = await readFile(path)

      if (buffer.includes(0)) {
        return
      }

      const lines = buffer.toString('utf-8').split('\n')
      const displayPath = relative(process.cwd(), path)

      for (let index = 0; index < lines.length; index++) {
        if (!lines[index].includes(input.query)) {
          continue
        }

        if (matchCount >= input.maxResults) {
          limited = true
          return
        }

        matchCount++

        const contextStart = Math.max(0, index - input.contextLines)
        const contextEnd = Math.min(
          lines.length - 1,
          index + input.contextLines,
        )

        for (let contextIndex = contextStart; contextIndex <= contextEnd; contextIndex++) {
          const key = `${displayPath}:${contextIndex + 1}`
          const existing = eventMap.get(key)

          eventMap.set(key, {
            path: displayPath,
            lineNumber: contextIndex + 1,
            content: lines[contextIndex],
            match: existing?.match || contextIndex === index,
          })
        }
      }
    } catch {
      return
    }
  }

  async function visit(directory: string): Promise<void> {
    if (matchCount >= input.maxResults) {
      limited = true
      return
    }

    let entries

    try {
      entries = await readdir(directory, { withFileTypes: true })
      entries.sort((left, right) => left.name.localeCompare(right.name))
    } catch {
      return
    }

    for (const entry of entries) {
      if (matchCount >= input.maxResults) {
        limited = true
        return
      }

      if (isBlockedName(entry.name)) {
        continue
      }

      const path = join(directory, entry.name)

      if (entry.isDirectory()) {
        await visit(path)
      } else if (entry.isFile()) {
        await searchFile(path)
      }
    }
  }

  if (targetStat.isDirectory()) {
    await visit(input.targetPath)
  } else {
    await searchFile(input.targetPath)
  }

  const events = [...eventMap.values()]

  if (events.length === 0) {
    return 'No matches found.'
  }

  const selectedKeys = new Set(
    events
      .filter((event) => event.match)
      .map((event) => `${event.path}:${event.lineNumber}`),
  )

  return formatSearchEvents({
    events,
    selectedKeys,
    limited,
    shownMatches: matchCount,
  })
}

function formatSearchEvents(input: {
  events: SearchEvent[]
  selectedKeys: Set<string>
  limited: boolean
  shownMatches: number
}) {
  const lines: string[] = []
  const seen = new Set<string>()
  let previous: SearchEvent | undefined

  for (const event of input.events) {
    const key = `${event.path}:${event.lineNumber}`

    if (seen.has(key)) {
      continue
    }

    seen.add(key)

    if (
      previous &&
      (previous.path !== event.path || event.lineNumber > previous.lineNumber + 1)
    ) {
      lines.push('--')
    }

    const separator = input.selectedKeys.has(key) ? ':' : '-'
    lines.push(`${event.path}${separator}${event.lineNumber}${separator}${event.content}`)
    previous = event
  }

  if (input.limited) {
    lines.push(`...result limit reached (${input.shownMatches} matches shown)`)
  }

  return truncateToolOutput(lines.join('\n'))
}
