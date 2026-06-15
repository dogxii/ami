import { clearLine, cursorTo } from 'node:readline'
import pc from 'picocolors'

const frameMs = 220
const frames = ['.', '..', '...']

export function createToolStatus() {
  let currentTool: string | undefined
  let frame = 0
  let timer: ReturnType<typeof setInterval> | undefined

  function getLine() {
    if (!currentTool) {
      return ''
    }

    return pc.dim(`⚙ ${currentTool}${frames[frame % frames.length]}`)
  }

  function render() {
    if (!process.stdout.isTTY || !currentTool) {
      return
    }

    clearLine(process.stdout, 0)
    cursorTo(process.stdout, 0)
    process.stdout.write(getLine())
    frame++
  }

  function start(toolName: string) {
    currentTool = toolName
    frame = 0

    if (!process.stdout.isTTY) {
      console.log(`tool: ${toolName}`)
      return
    }

    render()

    if (!timer) {
      timer = setInterval(render, frameMs)
    }
  }

  function stop() {
    if (timer) {
      clearInterval(timer)
      timer = undefined
    }

    if (process.stdout.isTTY && currentTool) {
      clearLine(process.stdout, 0)
      cursorTo(process.stdout, 0)
    }

    currentTool = undefined
  }

  return {
    start,
    stop,
  }
}

function sleep(ms: number) {
  return new Promise<void>((resolve) => setTimeout(resolve, ms))
}

async function runDemo() {
  const status = createToolStatus()

  status.start('⚙ read_file')
  await sleep(2400)

  status.start('⚙ list_files')
  await sleep(2400)

  status.start('⚙ read_file')
  await sleep(2400)

  status.stop()
}

if (import.meta.main) {
  await runDemo()
}
