import pc from 'picocolors'
import { generateReply } from '../llm/chat'
import { streamReply } from '../llm/streamChat'
import { getTool } from '../tools'
import { createToolStatus } from '../ui/toolStatus'
import { parseToolCall } from './toolCall'

const maxToolSteps = 5
const maxToolResultChars = 12_000

type RunAgentInput = {
  task: string
  model: string
  baseUrl: string
  apiKey: string
  tools?: {
    name: string
    description: string
  }[]
  debug: boolean
}

export async function runAgent(input: RunAgentInput) {
  // 输出空行
  console.log('')

  if (!input.apiKey) {
    console.log(pc.red('Missing AMI_API_KEY'))
    process.exitCode = 1
    return
  }

  const toolStatus = createToolStatus()

  try {
    const toolNames: string[] = []
    const toolResults: string[] = []
    let totalTokens = 0
    let hasTokenUsage = false
    let task = input.task

    for (let step = 0; step < maxToolSteps; step++) {
      const replyResult = await generateReply({ ...input, task })
      const reply = replyResult.content

      if (replyResult.totalTokens !== undefined) {
        totalTokens += replyResult.totalTokens
        hasTokenUsage = true
      }

      if (input.debug || process.env.AMI_DEBUG) {
        toolStatus.stop()
        console.log(pc.dim(`[debug] llm reply: ${reply}`))
      }

      const toolCall = parseToolCall(reply)

      // 最终回答
      if (!toolCall) {
        toolStatus.stop()

        const streamResult = await streamReply({
          task,
          baseUrl: input.baseUrl,
          model: input.model,
          apiKey: input.apiKey,
        })

        if (streamResult.totalTokens !== undefined) {
          totalTokens += streamResult.totalTokens
          hasTokenUsage = true
        }

        printRunSummary({
          model: input.model,
          toolNames,
          totalTokens: hasTokenUsage ? totalTokens : undefined,
        })

        return
      }

      const tool = getTool(toolCall.tool)

      if (!tool) {
        const message = `Tool not found: ${toolCall.tool}`

        if (input.debug || process.env.AMI_DEBUG) {
          toolStatus.stop()
          console.log(pc.red(message))
        }

        toolResults.push(
          buildToolResultText({
            toolName: toolCall.tool,
            result: message,
          }),
        )

        task = buildNextTask({
          originalTask: input.task,
          toolResults,
          instruction:
            'The previous tool name not found. Use available tool or reply.',
        })

        continue
      }

      toolNames.push(toolCall.tool)
      toolStatus.start(toolCall.tool)

      let result: unknown

      try {
        result = await tool.run(toolCall.input)
      } catch (error) {
        const message = error instanceof Error ? error.message : String(error)

        if (input.debug || process.env.AMI_DEBUG) {
          toolStatus.stop()
          console.log(pc.red(`tool failed: ${toolCall.tool}`))
          console.log(pc.dim(message))
        }

        toolResults.push(
          buildToolResultText({
            toolName: toolCall.tool,
            result: `Tool failed: ${message}`,
          }),
        )

        task = buildNextTask({
          originalTask: input.task,
          toolResults,
          instruction:
            'The previous tool call failed. Continue solving the original task. If you need another tool, call one. Otherwise answer the user.',
        })

        continue
      }

      toolResults.push(
        buildToolResultText({
          toolName: toolCall.tool,
          result,
        }),
      )

      task = buildNextTask({
        originalTask: input.task,
        toolResults,
        instruction:
          'Continue solving the original task. If you need another tool, call one. Otherwise answer the user.',
      })
    }

    toolStatus.stop()
    console.log(pc.red('Too many tool calls'))
    process.exitCode = 1
  } catch (error) {
    toolStatus.stop()
    console.log(pc.red('Failed to get LLM reply.'))

    if (error instanceof Error) {
      console.log(pc.dim(error.message))
    }

    process.exitCode = 1
    return
  }

  function formatToolSummary(toolNames: string[]) {
    if (toolNames.length === 0) {
      return ''
    }

    const counts = new Map<string, number>()

    for (const name of toolNames) {
      counts.set(name, (counts.get(name) ?? 0) + 1)
    }

    return [...counts.entries()]
      .map(([name, count]) => `${name} x${count}`)
      .join(', ')
  }

  function printRunSummary(input: {
    model: string
    toolNames: string[]
    totalTokens?: number
  }) {
    const toolSummary = formatToolSummary(input.toolNames)
    const parts = [input.model]

    if (toolSummary) {
      parts.push(toolSummary)
    }

    if (input.totalTokens !== undefined) {
      parts.push(`${input.totalTokens} tokens`)
    }

    console.log(pc.dim(parts.join(' · ')))
  }
}

function buildToolResultText(input: { toolName: string; result: unknown }) {
  const resultText = String(input.result)

  const clippedText =
    resultText.length > maxToolResultChars
      ? `${resultText.slice(0, maxToolResultChars)}\n\n...truncated`
      : resultText
  return [`Tool ${input.toolName} result:`, clippedText].join('\n\n')
}

function buildNextTask(input: {
  originalTask: string
  toolResults: string[]
  instruction: string
}) {
  return [
    `Original task: ${input.originalTask}`,
    'Tool results so far:',
    input.toolResults.join('\n\n'),
    input.instruction,
  ].join('\n\n')
}
