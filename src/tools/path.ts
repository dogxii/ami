import { relative, resolve } from 'node:path'

export function resolveInsideCwd(path: string) {
  const targetPath = resolve(process.cwd(), path)
  const relativePath = relative(process.cwd(), targetPath)

  if (relativePath.startsWith('..') || relativePath.startsWith('/')) {
    throw new Error('path must be inside current directory')
  }

  return {
    targetPath,
    relativePath,
  }
}

export function isBlockedPath(path: string) {
  return path.split(/[\\/]/).some(isBlockedName)
}

export function isBlockedName(name: string) {
  return (
    name === '.git' ||
    name === 'node_modules' ||
    name === '.env' ||
    name.startsWith('.env.')
  )
}
