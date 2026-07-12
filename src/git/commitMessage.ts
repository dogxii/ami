import { openResponse } from '../utils/request'

type GenerateCommitMessageInput = {
  status: string
  diff: string
  baseUrl: string
  model: string
  apiKey: string
}

export async function generateCommitMessage(input: GenerateCommitMessageInput) {
  const url = new URL('v1/chat/completions', input.baseUrl).toString()
  const request = await openResponse(url, {
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

  try {
    if (!request.response.ok) {
      if (request.response.status === 401 || request.response.status === 403) {
        throw new Error(
          'Authentication failed. Check your API key with `ami config`.',
        )
      }

      const errorText = await request.response.text()
      throw new Error(
        `Commit message request failed (${request.response.status}): ${errorText.slice(0, 300)}`,
      )
    }

    const data = (await request.response.json()) as {
      choices?: {
        message?: {
          content?: string
        }
      }[]
    }

    return data.choices?.[0]?.message?.content?.trim() ?? ''
  } finally {
    request.close()
  }
}
