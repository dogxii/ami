const retryStatuses = new Set([429, 500, 502, 503, 504])

type OpenResponseOptions = {
  timeoutMs?: number
  retries?: number
}

export type OpenResponseResult = {
  response: Response
  close: () => void
  getAbortError: () => RequestError | undefined
}

export class RequestError extends Error {
  constructor(
    message: string,
    readonly kind: 'cancelled' | 'network' | 'timeout',
  ) {
    super(message)
    this.name = 'RequestError'
  }
}

export async function openResponse(
  url: string,
  init: RequestInit,
  options: OpenResponseOptions = {},
): Promise<OpenResponseResult> {
  const retries = options.retries ?? 2

  for (let attempt = 0; attempt <= retries; attempt++) {
    const request = createRequestControl(options.timeoutMs ?? 60_000)

    try {
      const response = await fetch(url, {
        ...init,
        signal: request.controller.signal,
      })

      if (retryStatuses.has(response.status) && attempt < retries) {
        await response.body?.cancel()
        request.close()
        await wait(400 * 2 ** attempt)
        continue
      }

      return {
        response,
        close: request.close,
        getAbortError() {
          if (request.cancelled) {
            return new RequestError('Request cancelled.', 'cancelled')
          }

          if (request.timedOut) {
            return new RequestError(
              'Request timed out. Check your network or API base URL.',
              'timeout',
            )
          }
        },
      }
    } catch (error) {
      request.close()

      if (request.cancelled) {
        throw new RequestError('Request cancelled.', 'cancelled')
      }

      if (request.timedOut) {
        throw new RequestError(
          'Request timed out. Check your network or API base URL.',
          'timeout',
        )
      }

      if (attempt < retries) {
        await wait(400 * 2 ** attempt)
        continue
      }

      const detail = error instanceof Error ? ` ${error.message}` : ''
      throw new RequestError(
        `Unable to connect to the API.${detail}`,
        'network',
      )
    }
  }

  throw new RequestError('Unable to connect to the API.', 'network')
}

function createRequestControl(timeoutMs: number) {
  const controller = new AbortController()
  let cancelled = false
  let timedOut = false

  const timeout = setTimeout(() => {
    timedOut = true
    controller.abort()
  }, timeoutMs)

  const handleInterrupt = () => {
    cancelled = true
    controller.abort()
  }

  process.once('SIGINT', handleInterrupt)

  return {
    controller,
    get cancelled() {
      return cancelled
    },
    get timedOut() {
      return timedOut
    },
    close() {
      clearTimeout(timeout)
      process.removeListener('SIGINT', handleInterrupt)
    },
  }
}

function wait(ms: number) {
  return new Promise((resolve) => setTimeout(resolve, ms))
}
