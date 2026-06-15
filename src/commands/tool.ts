import pc from 'picocolors'
import { listTools } from '../tools'

export function registerToolCommand(cli: {
  command: (name: string, description?: string) => any
}) {
  cli
    .command('tool <name> [input]', 'Run a local tool manually')
    .action(async (name: string, input?: string) => {
      const { getTool } = await import('../tools')
      const tool = getTool(name)

      if (!tool) {
        console.log(pc.red(`Tool not found: ${name}`))
        process.exitCode = 1
        return
      }

      let toolInput: unknown

      try {
        toolInput = input ? JSON.parse(input) : {}
      } catch {
        console.log(pc.red('Invalid tool JSON input'))
        process.exitCode = 1
        return
      }

      try {
        const result = await tool.run(toolInput)
        console.log(result)
      } catch (error) {
        console.log(pc.red(`Tool failed: ${name}`))

        if (error instanceof Error) {
          console.log(pc.dim(error.message))
        }
        process.exitCode = 1
        return
      }
    })
}

export function registerToolsCommand(cli: {
  command: (name: string, description?: string) => any
}) {
  cli.command('tools', 'List all available local tools.').action(() => {
    for (const tool of listTools()) {
      console.log(`${tool.name} - ${tool.description}`)
    }
  })
}
