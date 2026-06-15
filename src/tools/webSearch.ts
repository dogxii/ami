import { loadConfig } from '../config/loadConfig'
import type { Tool } from './type'

type WebSearchInput = {
  query: string
}

type TavilyResult = {
  url?: string
  title?: string
  content?: string
  score?: number
}

type TavilyResponse = {
  results: TavilyResult[]
}

export const webSearchTool: Tool<WebSearchInput, string> = {
  name: 'web_search',
  description:
    'Search the web for recent or external information. Input JSON: {"query": "search keywords"}.',
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

    const response = await fetch('https://api.tavily.com/search', {
      method: 'POST',
      headers: {
        'Content-Type': 'application/json',
        Authorization: `Bearer ${apiKey}`,
      },
      body: JSON.stringify(body),
    })

    if (!response.ok) {
      const errorText = await response.text()
      throw new Error(
        `web_search failed: ${response.status} ${errorText.slice(0, 300)}`,
      )
    }

    const data = (await response.json()) as TavilyResponse

    const results = data.results ?? []

    if (results.length === 0) {
      return 'No search results found'
    }

    return results
      .map((result, index) =>
        [
          `${index + 1}. ${result.title ?? 'Untitled'}`,
          `URL: ${result.url ?? 'No URL'}`,
          `Content: ${result.content ?? 'No Content'}`,
          `Score: ${result.score ?? 'No Score'}`,
        ].join('\n'),
      )
      .join('\n\n')
  },
}
