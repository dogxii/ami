import { loadConfig } from '../config/loadConfig'
import { openResponse } from '../utils/request'
import { truncateToolOutput } from './limits'
import type { Tool } from './type'

type WebSearchInput = {
  query: string
}

type TavilyResult = {
  url?: string
  title?: string
  content?: string
}

type TavilyResponse = {
  results: TavilyResult[]
}

export const webSearchTool: Tool<WebSearchInput, string> = {
  name: 'web_search',
  description:
    'Search the web when recent or external information is required. Returns source titles, URLs, and snippets. Requires a Tavily API key.',
  parameters: {
    type: 'object',
    properties: {
      query: {
        type: 'string',
        description: 'Search keywords.',
      },
    },
    required: ['query'],
    additionalProperties: false,
  },
  async run(input) {
    if (
      typeof input !== 'object' ||
      input === null ||
      !('query' in input) ||
      typeof input.query !== 'string' ||
      !input.query.trim()
    ) {
      throw new Error('web_search requires input: {"query": "search keywords"}')
    }

    const apiKey = loadConfig().tavilyApiKey

    if (!apiKey) {
      throw new Error('Missing AMI_TAVILY_API_KEY')
    }

    const body = {
      query: input.query.trim(),
      max_results: 5,
      search_depth: 'basic',
    }

    const request = await openResponse('https://api.tavily.com/search', {
      method: 'POST',
      headers: {
        'Content-Type': 'application/json',
        Authorization: `Bearer ${apiKey}`,
      },
      body: JSON.stringify(body),
    })

    try {
      if (!request.response.ok) {
        const errorText = await request.response.text()
        throw new Error(
          `web_search failed (${request.response.status}): ${errorText.slice(0, 300)}`,
        )
      }

      const data = (await request.response.json()) as TavilyResponse

      const results = data.results ?? []

      if (results.length === 0) {
        return 'No search results found'
      }

      const output = results
        .map((result, index) =>
          [
            `${index + 1}. ${result.title ?? 'Untitled'}`,
            `URL: ${result.url ?? 'No URL'}`,
            `Content: ${result.content ?? 'No Content'}`,
          ].join('\n'),
        )
        .join('\n\n')

      return truncateToolOutput(output)
    } finally {
      request.close()
    }
  },
}
