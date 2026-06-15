export type ToolCallRequest = {
  tool: string
  input: unknown
}

export function parseToolCall(text: string): ToolCallRequest | null {
  try {
    const data = JSON.parse(normalizeToolCallText(text)) as unknown

    if (
      typeof data === 'object' &&
      data !== null &&
      'tool' in data &&
      typeof data.tool === 'string' &&
      'input' in data
    ) {
      return {
        tool: data.tool,
        input: data.input,
      }
    }

    return null
  } catch {
    return null
  }
}

function normalizeToolCallText(text: string) {
  return text
    .trim()
    .replace(/^```json\s*/i, '')
    .replace(/^```\s*/i, '')
    .replace(/```$/i, '')
    .trim()
}
