export function createTypeWriter() {
  let queue = ''
  let timer: ReturnType<typeof setInterval> | undefined
  let written = false

  function flush() {
    if (!queue) {
      return
    }

    const chunkSize = getChunkSize(queue.length)
    const chunk = queue.slice(0, chunkSize)
    queue = queue.slice(chunkSize)
    process.stdout.write(chunk)
    written = true
  }

  function start() {
    if (!timer) {
      timer = setInterval(flush, 16)
    }
  }

  async function stop() {
    while (queue) {
      flush()
      await new Promise((resolve) => setTimeout(resolve, 16))
    }

    if (timer) {
      clearInterval(timer)
      timer = undefined
    }

    if (written) {
      process.stdout.write('\n')
    }
  }

  function write(text: string) {
    queue += text
    start()
  }

  return {
    write,
    stop,
  }
}

export async function writeText(text: string) {
  const writer = createTypeWriter()
  writer.write(text)
  await writer.stop()
}

function getChunkSize(queueLength: number) {
  if (queueLength > 1000) return 80
  if (queueLength > 400) return 30
  if (queueLength > 120) return 10
  return 2
}
