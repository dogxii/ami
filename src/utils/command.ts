import { execFile } from 'node:child_process'

type CommandResult = {
  stdout: string
  stderr: string
  exitCode: number
  errorCode?: string | number
}

export function runCommand(
  command: string,
  args: string[] = [],
): Promise<CommandResult> {
  return new Promise((resolve) => {
    const child = execFile(
      command,
      args,
      {
        maxBuffer: 10 * 1024 * 1024,
      },
      (error, stdout, stderr) => {
        const errorCode = (error as { code?: unknown } | null)?.code
        const exitCode =
          typeof errorCode === 'number' ? errorCode : error ? 1 : 0

        resolve({
          stdout: String(stdout),
          stderr: String(stderr),
          exitCode,
          errorCode:
            typeof errorCode === 'string' || typeof errorCode === 'number'
              ? errorCode
              : undefined,
        })
      },
    )

    child.stdin?.end()
  })
}
