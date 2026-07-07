import { access } from 'node:fs/promises'
import { cancel, intro, isCancel, outro, select, text } from '@clack/prompts'
import { configPath } from './path'
import { saveConfig } from './saveConfig'

const providerPreset = {
  openai: {
    label: 'OpenAI',
    baseUrl: 'https://api.openai.com/',
    model: 'gpt-5.4-mini',
  },
  deepseek: {
    label: 'DeepSeek',
    baseUrl: 'https://api.deepseek.com/',
    model: 'deepseek-v4-flash',
  },
  qwen: {
    label: 'Qwen',
    baseUrl: 'https://dashscope.aliyuncs.com/compatible-mode/',
    model: 'qwen-plus',
  },
  custom: {
    label: 'Custom',
    baseUrl: '',
    model: '',
  },
} as const

type ProviderKeys = keyof typeof providerPreset

export async function initConfig() {
  intro('ami init')

  try {
    await access(configPath)
    cancel(
      'config.json already exists. Init cancelled to avoid overwriting it.',
    )
    process.exitCode = 1
    return
  } catch {}

  const provider = await select<ProviderKeys>({
    message: 'Choose provider',
    options: Object.entries(providerPreset).map(([value, preset]) => ({
      value: value as ProviderKeys,
      label: preset.label,
    })),
  })

  if (isCancel(provider)) {
    cancel('Init cancelled')
    process.exitCode = 1
    return
  }

  const preset = providerPreset[provider]

  const baseUrl = await text({
    message: 'Base URL',
    placeholder: preset.baseUrl || 'https://example.com/v1',
    defaultValue: preset.baseUrl,
    validate(value) {
      if (!value?.trim() && !preset.baseUrl) {
        return 'Base URL is required'
      }
    },
  })

  if (isCancel(baseUrl)) {
    cancel('Init cancelled')
    process.exitCode = 1
    return
  }

  const model = await text({
    message: 'Model',
    placeholder: preset.model || '',
    defaultValue: preset.model,
    validate(value) {
      if (!value?.trim() && !preset.model) {
        return 'Model is required'
      }
    },
  })

  if (isCancel(model)) {
    cancel('Init cancelled')
    process.exitCode = 1
    return
  }

  const apiKey = await text({
    message: 'API Key',
    validate(value) {
      if (!value?.trim()) {
        return 'API Key is required'
      }
    },
  })

  if (isCancel(apiKey)) {
    cancel('Init cancelled')
    process.exitCode = 1
    return
  }

  const tavilyApiKey = await text({
    message: 'Tavily API Key for web_search (optional)',
  })

  if (isCancel(tavilyApiKey)) {
    cancel('Init cancelled')
    process.exitCode = 1
    return
  }

  const config = {
    baseUrl: baseUrl.trim(),
    apiKey: apiKey.trim(),
    model: model.trim(),
    tavilyApiKey: tavilyApiKey.trim(),
  }

  await saveConfig(config)

  outro(`Create config.json for ${preset.label}. Run 'ami hello' to start it!`)
}
