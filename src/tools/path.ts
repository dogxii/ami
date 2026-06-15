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
