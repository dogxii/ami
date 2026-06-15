import type { AnyTool } from './type'

const tools: AnyTool[] = []

export function registerTool(tool: AnyTool) {
  tools.push(tool)
}

export function listTools() {
  return tools
}

export function getTool(name: string) {
  return tools.find((tool) => tool.name === name)
}
