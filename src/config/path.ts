import { homedir } from 'node:os'
import { dirname, join } from 'node:path'

export const configPath = join(homedir(), '.config', 'ami', 'config.json')
export const configDir = dirname(configPath)
