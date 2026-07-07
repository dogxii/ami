import { readFileSync } from 'node:fs'
import { configPath } from './path'

export type AmiConfig = {
  baseUrl: string
  apiKey: string
  model: string
  tavilyApiKey: string
}

export function loadConfig(): AmiConfig {
  const fileConfig = readConfigFile()

  return {
    baseUrl:
      process.env.AMI_BASE_URL ??
      fileConfig.baseUrl ??
      'https://api.deepseek.com/',
    apiKey: process.env.AMI_API_KEY ?? fileConfig.apiKey ?? '',
    model: process.env.AMI_MODEL ?? fileConfig.model ?? 'deepseek-v4-flash',
    tavilyApiKey:
      process.env.AMI_TAVILY_API_KEY ?? fileConfig.tavilyApiKey ?? '',
  }
}

export type ConfigFile = Partial<AmiConfig>

export function readConfigFile(): ConfigFile {
  try {
    const content = readFileSync(configPath, 'utf-8')
    return JSON.parse(content) as ConfigFile
  } catch {
    return {}
  }
}
