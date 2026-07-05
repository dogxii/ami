import { createTypeWriter } from '../ui/typeWriter'
import {
  openResponse,
  type OpenResponseResult,
} from '../utils/request'
import type { ToolParameters } from '../tools/type'

export type ChatTool = {
  name: string
  description: string
  parameters: ToolParameters
}

export type AssistantToolCall = {
  id: string
  type: 'function'
  function: {
    name: string
    arguments: string
  }
}

export type ChatMessage =
  | {
      role: 'system' | 'user'
      content: string
    }
  | {
      role: 'assistant'
      content: string | null
      tool_calls?: AssistantToolCall[]
    }
  | {
      role: 'tool'
      content: string
      tool_call_id: string
    }

type StreamChatInput = {
  messages: ChatMessage[]
  baseUrl: string
  model: string
  apiKey: string
  tools: ChatTool[]
}

export type StreamChatResult =
  | {
      type: 'message'
      content: string
      totalTokens?: number
    }
  | {
      type: 'tool_calls'
      assistantMessage: ChatMessage
      toolCalls: {
        id: string
        name: string
        input: unknown
      }[]
      totalTokens?: number
    }

type GenerateReplyInput = {
  task: string
  baseUrl: string
  model: string
  apiKey: string
  tools: ChatTool[]
}

type GenerateReplyResult = {
  content: string
  totalTokens?: number
}

type StreamChunk = {
  choices?: {
    delta?: {
      content?: string | null
      tool_calls?: {
        index?: number
        id?: string
        type?: 'function'
        function?: {
          name?: string
          arguments?: string
        }
      }[]
    }
  }[]
  usage?: {
    total_tokens?: number
  }
}

type ChatCompletionResponse = {
  choices?: {
    message?: {
      content?: string
    }
  }[]
  usage?: {
    total_tokens?: number
  }
}

type PendingToolCall = {
  id: string
  name: string
  arguments: string
}

export class LlmRequestError extends Error {
  constructor(
    message: string,
    readonly status: number,
    readonly details: string,
  ) {
    super(message)
    this.name = 'LlmRequestError'
  }
}

export async function streamChat(
  input: StreamChatInput,
): Promise<StreamChatResult> {
  let request: OpenResponseResult

  try {
    request = await openChatResponse(input, true)
  } catch (error) {
    if (isStreamOptionsUnsupported(error)) {
      request = await openChatResponse(input, false)
    } else {
      throw error
    }
  }

  const writer = createTypeWriter()
  const decoder = new TextDecoder()
  const toolCalls = new Map<number, PendingToolCall>()
  let buffer = ''
  let content = ''
  let totalTokens: number | undefined

  function readEvent(line: string) {
    const trimmedLine = line.trim()

    if (!trimmedLine.startsWith('data:')) {
      return false
    }

    const dataText = trimmedLine.slice('data:'.length).trim()

    if (dataText === '[DONE]') {
      return true
    }

    let data: StreamChunk

    try {
      data = JSON.parse(dataText) as StreamChunk
    } catch {
      return false
    }

    if (data.usage?.total_tokens !== undefined) {
      totalTokens = data.usage.total_tokens
    }

    const delta = data.choices?.[0]?.delta

    if (delta?.content) {
      content += delta.content
      writer.write(delta.content)
    }

    for (const toolCall of delta?.tool_calls ?? []) {
      const index = toolCall.index ?? 0
      const current = toolCalls.get(index) ?? {
        id: '',
        name: '',
        arguments: '',
      }

      current.id += toolCall.id ?? ''
      current.name += toolCall.function?.name ?? ''
      current.arguments += toolCall.function?.arguments ?? ''
      toolCalls.set(index, current)
    }

    return false
  }

  try {
    if (!request.response.body) {
      throw new Error('LLM stream response has no body.')
    }

    const reader = request.response.body.getReader()
    let streamDone = false

    while (!streamDone) {
      const { value, done } = await reader.read()

      if (done) {
        buffer += decoder.decode()
        break
      }

      buffer += decoder.decode(value, { stream: true })
      const lines = buffer.split('\n')
      buffer = lines.pop() ?? ''

      for (const line of lines) {
        if (readEvent(line)) {
          streamDone = true
          break
        }
      }
    }

    if (buffer) {
      readEvent(buffer)
    }
  } catch (error) {
    const abortError = request.getAbortError()

    if (abortError) {
      throw abortError
    }

    throw error
  } finally {
    request.close()
    await writer.stop()
  }

  if (toolCalls.size > 0) {
    const calls = [...toolCalls.entries()]
      .sort(([left], [right]) => left - right)
      .map(([, call], index) => ({
        id: call.id || `call_${index}`,
        name: call.name,
        arguments: call.arguments || '{}',
      }))

    const assistantToolCalls: AssistantToolCall[] = calls.map((call) => ({
      id: call.id,
      type: 'function',
      function: {
        name: call.name,
        arguments: call.arguments,
      },
    }))

    return {
      type: 'tool_calls',
      assistantMessage: {
        role: 'assistant',
        content: content || null,
        tool_calls: assistantToolCalls,
      },
      toolCalls: calls.map((call) => ({
        id: call.id,
        name: call.name,
        input: parseToolInput(call.arguments),
      })),
      totalTokens,
    }
  }

  if (!content) {
    throw new Error('The model returned an empty response.')
  }

  return {
    type: 'message',
    content,
    totalTokens,
  }
}

export async function generateReply(
  input: GenerateReplyInput,
): Promise<GenerateReplyResult> {
  const toolText = [
    '',
    'Available tools:',
    ...input.tools.map((tool) => `${tool.name} - ${tool.description}`),
    '',
    'If you need to use a tool, respond with only JSON in this format:',
    '{"tool":"tool_name","input":{}}',
    'Use exactly one of the available tool names.',
    'You can request one tool call at a time.',
    'Do not wrap tool calls in markdown.',
  ].join('\n')

  const request = await openResponse(
    getChatUrl(input.baseUrl),
    {
      method: 'POST',
      headers: getHeaders(input.apiKey),
      body: JSON.stringify({
        model: input.model,
        messages: [
          {
            role: 'system',
            content: `${createSystemPrompt()}\n${toolText}`,
          },
          {
            role: 'user',
            content: input.task,
          },
        ],
      }),
    },
  )

  try {
    await assertOk(request.response)
    const data = (await request.response.json()) as ChatCompletionResponse

    return {
      content: data.choices?.[0]?.message?.content ?? '',
      totalTokens: data.usage?.total_tokens,
    }
  } finally {
    request.close()
  }
}

export function createSystemPrompt() {
  return [
    'You are ami, a concise terminal assistant. Answer directly and keep responses short.',
    `Current Date: ${getCurrentLocalDate()}.`,
  ].join('\n')
}

export function isNativeToolsUnsupported(error: unknown) {
  return (
    error instanceof LlmRequestError &&
    (error.status === 400 || error.status === 422) &&
    /(tool|function).*(unsupported|unknown|invalid|not supported)|(unsupported|unknown).*(tool|function)/i.test(
      error.details,
    )
  )
}

async function openChatResponse(
  input: StreamChatInput,
  includeUsage: boolean,
) {
  const body: Record<string, unknown> = {
    model: input.model,
    stream: true,
    messages: input.messages,
    tools: input.tools.map((tool) => ({
      type: 'function',
      function: {
        name: tool.name,
        description: tool.description,
        parameters: tool.parameters,
      },
    })),
    tool_choice: 'auto',
  }

  if (includeUsage) {
    body.stream_options = {
      include_usage: true,
    }
  }

  const request = await openResponse(getChatUrl(input.baseUrl), {
    method: 'POST',
    headers: getHeaders(input.apiKey),
    body: JSON.stringify(body),
  })

  try {
    await assertOk(request.response)
    return request
  } catch (error) {
    request.close()
    throw error
  }
}

async function assertOk(response: Response) {
  if (response.ok) {
    return
  }

  const details = (await response.text()).slice(0, 500)
  throw new LlmRequestError(
    getHttpErrorMessage(response.status),
    response.status,
    details,
  )
}

function getHttpErrorMessage(status: number) {
  if (status === 401 || status === 403) {
    return 'Authentication failed. Check your API key with `ami config`.'
  }

  if (status === 404) {
    return 'Model or API endpoint not found. Check `model` and `baseUrl` with `ami config`.'
  }

  if (status === 429) {
    return 'The API rate limit was reached. Try again in a moment.'
  }

  if (status >= 500) {
    return `The API service is unavailable (${status}). Try again later.`
  }

  return `LLM request failed (${status}). Check your model and API configuration.`
}

function isStreamOptionsUnsupported(error: unknown) {
  return (
    error instanceof LlmRequestError &&
    (error.status === 400 || error.status === 422) &&
    /stream_options|include_usage/i.test(error.details)
  )
}

function parseToolInput(value: string): unknown {
  try {
    return JSON.parse(value) as unknown
  } catch {
    return value
  }
}

function getChatUrl(baseUrl: string) {
  return new URL('v1/chat/completions', baseUrl).toString()
}

function getHeaders(apiKey: string) {
  return {
    'Content-Type': 'application/json',
    Authorization: `Bearer ${apiKey}`,
  }
}

function getCurrentLocalDate() {
  const now = new Date()
  const year = now.getFullYear()
  const month = String(now.getMonth() + 1).padStart(2, '0')
  const day = String(now.getDate()).padStart(2, '0')

  return `${year}-${month}-${day}`
}
