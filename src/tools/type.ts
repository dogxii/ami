export type ToolParameters = {
  type: 'object'
  properties?: Record<string, Record<string, unknown>>
  required?: string[]
  additionalProperties?: boolean
}

export type Tool<Input = unknown, Output = unknown> = {
  name: string
  description: string
  parameters: ToolParameters
  run: (input: Input) => Promise<Output>
}

export type AnyTool = Tool<any, any>
