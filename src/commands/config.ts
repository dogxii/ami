import pc from 'picocolors'
import { loadConfig } from '../config/loadConfig'
import { configPath } from '../config/path'
import { type ConfigField, updateConfig } from '../config/saveConfig'

const configFields = ['baseUrl', 'model', 'apiKey', 'tavilyApiKey'] as const

export function registerConfigCommand(cli: {
  command: (name: string, description?: string) => any
}) {
  cli
    .command('config [...args]', 'Show or edit ami configuration')
    .action(async (args: string[] = []) => {
      const [action, key, value] = args

      if (!action) {
        const config = loadConfig()

        console.log(pc.bold('ami config'))
        console.log(`model: ${config.model}`)
        console.log(`baseUrl: ${config.baseUrl}`)
        console.log(`apiKey: ${maskSecret(config.apiKey)}`)
        console.log(`tavilyApiKey: ${maskSecret(config.tavilyApiKey ?? '')}`)
        console.log('')
        console.log(`config saved in ${configPath}`)
        return
      }

      if (action === 'get') {
        if (!key) {
          console.log(pc.red('Usage: ami config get <key>'))
          process.exitCode = 1
          return
        }

        if (!isConfigField(key)) {
          console.log(pc.red(`Unknown config key: ${key}`))
          console.log(pc.dim(`Available keys: ${configFields.join(', ')}`))
          process.exitCode = 1
          return
        }

        const config = loadConfig()
        const value = config[key]

        console.log(
          key === 'apiKey' || key === 'tavilyApiKey'
            ? maskSecret(value)
            : value,
        )
        return
      }

      if (action !== 'set') {
        console.log(pc.red(`Unknown config action: ${action}`))
        process.exitCode = 1
        return
      }

      if (!key || value === undefined) {
        console.log(pc.red('Usage: ami config set <key> <value>'))
        process.exitCode = 1
        return
      }

      if (!isConfigField(key)) {
        console.log(pc.red(`Unknown config key: ${key}`))
        console.log(pc.dim(`Available keys: ${configFields.join(', ')}`))
        process.exitCode = 1
        return
      }

      if (!value.trim()) {
        console.log(pc.red('Config value cannot be empty'))
        process.exitCode = 1
        return
      }

      await updateConfig({ [key]: value })
      console.log(`Updated ${key}`)
    })
}

function maskSecret(value: string) {
  if (!value) {
    return 'missing'
  }

  if (value.length <= 8) {
    return '*'.repeat(value.length)
  }

  return `${value.slice(0, 5)}****${value.slice(-4)}`
}

function isConfigField(key: string): key is ConfigField {
  return configFields.includes(key as ConfigField)
}
