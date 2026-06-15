export type Tool<Input = unknown, Output = unknown> = {
  name: string
  description: string
  run: (input: Input) => Promise<Output>
}

export type AnyTool = Tool<any, any>
