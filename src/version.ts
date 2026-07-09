import { readFileSync } from 'node:fs'

type PackageJson = {
  version?: string
}

const packageJson = JSON.parse(
  readFileSync(new URL('../package.json', import.meta.url), 'utf-8'),
) as PackageJson

export const version = packageJson.version ?? '0.0.0'
