import { spawn } from 'node:child_process'
import { confirm, isCancel } from '@clack/prompts'
import pc from 'picocolors'
import { getLatestVersion, isNewerVersion, packageName } from '../update'
import { version } from '../version'

type UpdateOptions = {
  yes?: boolean
}

export function registerUpdateCommand(cli: {
  command: (name: string, description?: string) => any
}) {
  cli
    .command('update', 'Update ami to the latest version')
    .option('--yes', 'Skip update confirmation')
    .action(async (options: UpdateOptions) => {
      try {
        const latestVersion = await getLatestVersion({
          force: true,
          timeout: 5_000,
        })

        console.log(`Current: ${version}`)
        console.log(`Latest:  ${latestVersion}`)

        if (!isNewerVersion(latestVersion, version)) {
          console.log(pc.dim('Ami is up to date'))
          return
        }

        if (!options.yes) {
          if (!process.stdin.isTTY) {
            console.log(pc.dim('Run ami update --yes to update without prompting.'))
            process.exitCode = 1
            return
          }

          const shouldUpdate = await confirm({
            message: `Update ami to ${latestVersion}?`,
          })

          if (isCancel(shouldUpdate) || !shouldUpdate) {
            console.log(pc.dim('Update canceled'))
            return
          }
        }

        console.log('')
        console.log(pc.dim(`Installing ${packageName}@latest...`))
        await installLatestVersion()
        console.log(pc.dim(`Updated to ${latestVersion}`))
      } catch (error) {
        console.log(pc.red('Failed to update ami'))

        if (error instanceof Error) {
          console.log(error.message)
        }

        process.exitCode = 1
      }
    })
}

function installLatestVersion() {
  const npm = process.platform === 'win32' ? 'npm.cmd' : 'npm'

  return new Promise<void>((resolve, reject) => {
    const child = spawn(npm, ['install', '-g', `${packageName}@latest`], {
      stdio: 'inherit',
    })

    child.once('error', reject)
    child.once('close', (code, signal) => {
      if (code === 0) {
        resolve()
        return
      }

      reject(
        new Error(
          signal
            ? `npm was stopped by ${signal}`
            : `npm install exited with code ${code ?? 'unknown'}`,
        ),
      )
    })
  })
}
