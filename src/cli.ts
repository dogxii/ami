import { cac } from 'cac'
import pc from 'picocolors'
import { runAgent } from './agent/runAgent'
import { registerCommitCommand } from './commands/commit'
import { registerConfigCommand } from './commands/config'
import { registerPushCommand } from './commands/push'
import { registerToolCommand, registerToolsCommand } from './commands/tool'
import { initConfig } from './config/initConfig'
import { loadConfig } from './config/loadConfig'
import { listTools } from './tools'
import { buildTaskWithStdin, readStdin } from './utils/stdin'

const cli = cac('ami')

cli
  .command('[...task]', 'Run a natural language task in the terminal')
  .option('--model <model>', 'Override the model name')
  .option('--debug', 'Print LLM raw replies')
  .action(
    async (
      taskParts: string[] = [],
      options: { model?: string; debug?: boolean },
    ) => {
      const taskText = taskParts.join(' ').trim()
      const stdin = await readStdin()
      const task = buildTaskWithStdin(taskText, stdin.content)

      if (stdin.truncated) {
        console.error(pc.dim('stdin was truncated to 100000 characters'))
      }

      if (!task) {
        console.log(pc.bold('ami'))
        console.log('Usage: ami <task>')
        console.log('')
        console.log('Examples:')
        console.log('  ami explain package.json')
        console.log('  ami summarize git status')
        return
      }

      const config = loadConfig()

      await runAgent({
        task,
        model: options.model ?? config.model,
        baseUrl: config.baseUrl,
        apiKey: config.apiKey,
        tools: listTools().map((tool) => ({
          name: tool.name,
          description: tool.description,
          parameters: tool.parameters,
        })),
        debug: options.debug ?? false,
      })
    },
  )

cli.command('init', 'Create ami config').action(async () => {
  await initConfig()
})

registerConfigCommand(cli)
registerToolsCommand(cli)
registerToolCommand(cli)
registerCommitCommand(cli)
registerPushCommand(cli)

cli.help()
cli.version('0.1.0')
cli.parse()
