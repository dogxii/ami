type GenerateReplyInput = {
  task: string
  baseUrl: string
  model: string
  apiKey: string
  tools?: {
    name: string
    description: string
  }[]
}

type GenerateReplyResult = {
  content: string
  totalTokens?: number
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

export async function generateReply(
  input: GenerateReplyInput,
): Promise<GenerateReplyResult> {
  const url = new URL('v1/chat/completions', input.baseUrl).toString()
  const toolText =
    input.tools && input.tools.length > 0
      ? [
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
      : ''
  const response = await fetch(url, {
    method: 'POST',
    headers: {
      'Content-Type': 'application/json',
      Authorization: `Bearer ${input.apiKey}`,
    },
    body: JSON.stringify({
      model: input.model,
      messages: [
        {
          role: 'system',
          content: [
            'You are ami, a concise terminal assistant. Answer directly and keep responses short.',
            `Current Date: ${getCurrentLocalDate()}.`,
            toolText,
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

  const data = (await response.json()) as ChatCompletionResponse
  const reply = data.choices?.[0]?.message?.content ?? ''

  return {
    content: reply,
    totalTokens: data.usage?.total_tokens,
  }
}

function getCurrentLocalDate() {
  const now = new Date()
  const year = now.getFullYear()
  const month = String(now.getMonth() + 1).padStart(2, '0')
  const day = String(now.getDate()).padStart(2, '0')

  return `${year}-${month}-${day}`
}
