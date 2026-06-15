import { listTools } from './registry'
import type { Tool } from './type'

export const listToolsTool: Tool<Record<string, never>, string> = {
  name: 'list_tools',
  description: 'List all available local tools.',
  async run() {
    return listTools()
      .map((tool) => `${tool.name} - ${tool.description}`)
      .join('\n')
  },
}
