type StreamReplyInput = {
  task: string
  baseUrl: string
  model: string
  apiKey: string
}

type StreamReplyResult = {
  totalTokens?: number
}

type StreamChunk = {
  choices?: {
    delta?: {
      content?: string
    }
  }[]
  usage?: {
    total_tokens?: number
  }
}

export async function streamReply(
  input: StreamReplyInput,
): Promise<StreamReplyResult> {
  const url = new URL('v1/chat/completions', input.baseUrl).toString()
  const response = await fetch(url, {
    method: 'POST',
    headers: {
      'Content-Type': 'application/json',
      Authorization: `Bearer ${input.apiKey}`,
    },
    body: JSON.stringify({
      model: input.model,
      stream: true,
      stream_options: {
        include_usage: true,
      },
      messages: [
        {
          role: 'system',
          content: [
            'You are ami, a concise terminal assistant. Answer directly and keep responses short.',
            `Current Date: ${getCurrentLocalDate()}.`,
          ].join('\n'),
        },
        {
          role: 'user',
          content: input.task,
        },
      ],
    }),
  })

  if (!response.ok) {
    const errorText = await response.text()
    throw new Error(
      `LLM request failed: ${response.status} ${errorText.slice(0, 300)}`,
    )
  }

  if (!response.body) {
    throw new Error(`LLM stream response has no body`)
  }

  const reader = response.body.getReader()
  const decoder = new TextDecoder()
  const writer = createTypeWriter()

  let buffer = ''
  let totalTokens: number | undefined

  while (true) {
    const { value, done } = await reader.read()

    if (done) {
      break
    }

    buffer += decoder.decode(value, { stream: true })

    const lines = buffer.split('\n')
    buffer = lines.pop() ?? ''

    for (const line of lines) {
      const trimmedLine = line.trim()

      if (!trimmedLine.startsWith('data:')) {
        continue
      }

      const dataText = trimmedLine.slice('data:'.length).trim()

      if (dataText === '[DONE]') {
        await writer.stop()
        return { totalTokens }
      }

      let data: StreamChunk

      try {
        data = JSON.parse(dataText) as StreamChunk
      } catch {
        continue
      }

      if (data.usage?.total_tokens !== undefined) {
        totalTokens = data.usage.total_tokens
      }

      const content = data.choices?.[0]?.delta?.content

      if (content) {
        writer.write(content)
      }
    }
  }

  await writer.stop()
  return { totalTokens }
}

function getCurrentLocalDate() {
  const now = new Date()
  const year = now.getFullYear()
  const month = String(now.getMonth() + 1).padStart(2, '0')
  const day = String(now.getDate()).padStart(2, '0')

  return `${year}-${month}-${day}`
}

function createTypeWriter() {
  let queue = ''
  let timer: ReturnType<typeof setInterval> | undefined

  function flush() {
    if (!queue) {
      return
    }

    const chunkSize = getChunkSize(queue.length)
    const chunk = queue.slice(0, chunkSize)
    queue = queue.slice(chunkSize)
    process.stdout.write(chunk)
  }

  function start() {
    if (!timer) {
      timer = setInterval(flush, 16)
    }
  }

  async function stop() {
    while (queue) {
      flush()
      await new Promise((resolve) => setTimeout(resolve, 16))
    }

    if (timer) {
      clearInterval(timer)
      timer = undefined
    }

    process.stdout.write('\n')
  }

  function write(text: string) {
    queue += text
    start()
  }

  return {
    write,
    stop,
  }
}

function getChunkSize(queueLength: number) {
  if (queueLength > 1000) return 80
  if (queueLength > 400) return 30
  if (queueLength > 120) return 10
  return 2
}
