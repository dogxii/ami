import pc from 'picocolors'
import {
  createSystemPrompt,
  generateReply,
  isNativeToolsUnsupported,
  LlmRequestError,
  streamChat,
  type ChatMessage,
  type ChatTool,
} from '../llm/chat'
import { getTool } from '../tools'
import { truncateToolOutput } from '../tools/limits'
import { createToolStatus } from '../ui/toolStatus'
import { writeText } from '../ui/typeWriter'
import { RequestError } from '../utils/request'
import { parseToolCall } from './toolCall'

const maxToolCalls = 5

type RunAgentInput = {
  task: string
  model: string
  baseUrl: string
  apiKey: string
  tools?: ChatTool[]
  debug: boolean
}

type RunState = {
  toolNames: string[]
  totalTokens: number
  hasTokenUsage: boolean
  toolCallCount: number
}

export async function runAgent(input: RunAgentInput) {
  console.log('')

  if (!input.apiKey) {
    console.log(pc.red('Missing API key. Run `ami init` or set AMI_API_KEY.'))
    process.exitCode = 1
    return
  }

  const toolStatus = createToolStatus()
  const state: RunState = {
    toolNames: [],
    totalTokens: 0,
    hasTokenUsage: false,
    toolCallCount: 0,
  }

  try {
    const completedWithNativeTools = await runNativeAgent(
      input,
      state,
      toolStatus,
    )

    if (!completedWithNativeTools) {
      await runFallbackAgent(input, state, toolStatus)
    }
  } catch (error) {
    toolStatus.stop()
    printError(error, input.debug)
    process.exitCode = 1
  }
}

async function runNativeAgent(
  input: RunAgentInput,
  state: RunState,
  toolStatus: ReturnType<typeof createToolStatus>,
) {
  const tools = input.tools ?? []
  const messages: ChatMessage[] = [
    {
      role: 'system',
      content: createSystemPrompt(),
    },
    {
      role: 'user',
      content: input.task,
    },
  ]

  while (state.toolCallCount < maxToolCalls) {
    let reply: Awaited<ReturnType<typeof streamChat>>

    try {
      reply = await streamChat({
        messages,
        baseUrl: input.baseUrl,
        model: input.model,
        apiKey: input.apiKey,
        tools,
      })
    } catch (error) {
      if (messages.length === 2 && isNativeToolsUnsupported(error)) {
        if (isDebug(input)) {
          console.log(pc.dim('[debug] native tools unsupported, using JSON'))
        }

        return false
      }

      throw error
    }

    addTokenUsage(state, reply.totalTokens)

    if (reply.type === 'message') {
      printRunSummary(input.model, state)
      return true
    }

    if (isDebug(input)) {
      console.log(pc.dim(`[debug] tool calls: ${JSON.stringify(reply.toolCalls)}`))
    }

    messages.push(reply.assistantMessage)

    for (const toolCall of reply.toolCalls) {
      ensureToolCallLimit(state)
      state.toolCallCount++

      const result = await executeTool(
        toolCall.name,
        toolCall.input,
        state,
        toolStatus,
        input,
      )

      messages.push({
        role: 'tool',
        tool_call_id: toolCall.id,
        content: result,
      })
    }
  }

  throw new Error('Too many tool calls. Try a more specific request.')
}

async function runFallbackAgent(
  input: RunAgentInput,
  state: RunState,
  toolStatus: ReturnType<typeof createToolStatus>,
) {
  const toolResults: string[] = []
  let task = input.task

  while (state.toolCallCount < maxToolCalls) {
    const replyResult = await generateReply({
      ...input,
      task,
      tools: input.tools ?? [],
    })
    const reply = replyResult.content

    addTokenUsage(state, replyResult.totalTokens)

    if (isDebug(input)) {
      console.log(pc.dim(`[debug] llm reply: ${reply}`))
    }

    const toolCall = parseToolCall(reply)

    if (!toolCall) {
      if (!reply) {
        throw new Error('The model returned an empty response.')
      }

      await writeText(reply)
      printRunSummary(input.model, state)
      return
    }

    ensureToolCallLimit(state)
    state.toolCallCount++

    const result = await executeTool(
      toolCall.tool,
      toolCall.input,
      state,
      toolStatus,
      input,
    )

    toolResults.push(
      buildToolResultText({
        toolName: toolCall.tool,
        result,
      }),
    )

    task = [
      `Original task: ${input.task}`,
      'Tool results so far:',
      toolResults.join('\n\n'),
      'Continue solving the original task. If you need another tool, call one. Otherwise answer the user.',
    ].join('\n\n')
  }

  throw new Error('Too many tool calls. Try a more specific request.')
}

async function executeTool(
  name: string,
  input: unknown,
  state: RunState,
  toolStatus: ReturnType<typeof createToolStatus>,
  agentInput: RunAgentInput,
) {
  const tool = getTool(name)

  if (!tool) {
    return `Tool not found: ${name}`
  }

  state.toolNames.push(name)
  toolStatus.start(name)

  try {
    const result = await tool.run(input)
    return clipToolResult(result)
  } catch (error) {
    const message = error instanceof Error ? error.message : String(error)

    if (isDebug(agentInput)) {
      toolStatus.stop()
      console.log(pc.red(`tool failed: ${name}`))
      console.log(pc.dim(message))
    }

    return `Tool failed: ${message}`
  } finally {
    toolStatus.stop()
  }
}

function addTokenUsage(state: RunState, totalTokens?: number) {
  if (totalTokens === undefined) {
    return
  }

  state.totalTokens += totalTokens
  state.hasTokenUsage = true
}

function ensureToolCallLimit(state: RunState) {
  if (state.toolCallCount >= maxToolCalls) {
    throw new Error('Too many tool calls. Try a more specific request.')
  }
}

function clipToolResult(result: unknown) {
  return truncateToolOutput(String(result))
}

function buildToolResultText(input: { toolName: string; result: unknown }) {
  return [`Tool ${input.toolName} result:`, clipToolResult(input.result)].join(
    '\n\n',
  )
}

function printRunSummary(model: string, state: RunState) {
  const toolSummary = formatToolSummary(state.toolNames)
  const parts = [model]

  if (toolSummary) {
    parts.push(toolSummary)
  }

  if (state.hasTokenUsage) {
    parts.push(`${state.totalTokens} tokens`)
  }

  console.log(pc.dim(parts.join(' · ')))
}

function formatToolSummary(toolNames: string[]) {
  const counts = new Map<string, number>()

  for (const name of toolNames) {
    counts.set(name, (counts.get(name) ?? 0) + 1)
  }

  return [...counts.entries()]
    .map(([name, count]) => `${name} x${count}`)
    .join(', ')
}

function printError(error: unknown, debug: boolean) {
  if (error instanceof RequestError && error.kind === 'cancelled') {
    console.log(pc.dim(error.message))
    return
  }

  const message = error instanceof Error ? error.message : String(error)
  console.log(pc.red(message))

  if (debug && error instanceof LlmRequestError && error.details) {
    console.log(pc.dim(error.details))
  }
}

function isDebug(input: RunAgentInput) {
  return input.debug || Boolean(process.env.AMI_DEBUG)
}
