import { mkdir, writeFile } from 'node:fs/promises'
import { loadConfig } from './loadConfig'
import { configDir, configPath } from './path'

export type ConfigField = 'baseUrl' | 'model' | 'apiKey' | 'tavilyApiKey'

type AmiConfigFile = Partial<Record<ConfigField, string>>

export async function saveConfig(config: AmiConfigFile) {
  await mkdir(configDir, { recursive: true })

  const content = `${JSON.stringify(config, null, 2)}\n`
  await writeFile(configPath, content, 'utf-8')
}

export async function updateConfig(config: AmiConfigFile) {
  const currentConfig = loadConfig()

  await saveConfig({ ...currentConfig, ...config })
}
