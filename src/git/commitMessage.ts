type GenerateCommitMessageInput = {
  status: string
  diff: string
  baseUrl: string
  model: string
  apiKey: string
}

export async function generateCommitMessage(input: GenerateCommitMessageInput) {
  const url = new URL('v1/chat/completions', input.baseUrl).toString()
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
            'You generate concise Conventional Commit messages.',
            'Return only one commit message, no markdown, no quotes.',
            '',
            'Rules:',
            '- Use format: type(scope): summary',
            '- Type should be one of: feat, fix, refactor, chore, docs, test, style',
            '- Keep summary under 72 characters',
            '- Use lowercase English',
          ].join('\n'),
        },
        {
          role: 'user',
          content: [
            'Generate a commit message for these changes.',
            '',
            'Git status:',
            input.status,
            '',
            'Git diff:',
            input.diff,
          ].join('\n'),
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

  const data = (await response.json()) as {
    choices?: {
      message?: {
        content?: string
      }
    }[]
  }

  return data.choices?.[0]?.message?.content?.trim() ?? ''
}
