import { chmod, mkdir, writeFile } from 'node:fs/promises'
import { readConfigFile } from './loadConfig'
import { configDir, configPath } from './path'

export type ConfigField = 'baseUrl' | 'model' | 'apiKey' | 'tavilyApiKey'

type AmiConfigFile = Partial<Record<ConfigField, string>>

export async function saveConfig(config: AmiConfigFile) {
  await mkdir(configDir, { recursive: true, mode: 0o700 })

  const content = `${JSON.stringify(config, null, 2)}\n`
  await writeFile(configPath, content, {
    encoding: 'utf-8',
    mode: 0o600,
  })

  if (process.platform !== 'win32') {
    await chmod(configDir, 0o700)
    await chmod(configPath, 0o600)
  }
}

export async function updateConfig(config: AmiConfigFile) {
  const currentConfig = readConfigFile()

  await saveConfig({ ...currentConfig, ...config })
}
