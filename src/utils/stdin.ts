const maxStdinChars = 100_000

type StdinInput = {
  content: string
  truncated: boolean
}

export async function readStdin(): Promise<StdinInput> {
  if (process.stdin.isTTY) {
    return {
      content: '',
      truncated: false,
    }
  }

  process.stdin.setEncoding('utf-8')

  let content = ''
  let truncated = false

  for await (const chunk of process.stdin) {
    if (content.length >= maxStdinChars) {
      truncated = true
      continue
    }

    const remaining = maxStdinChars - content.length
    content += String(chunk).slice(0, remaining)

    if (String(chunk).length > remaining) {
      truncated = true
    }
  }

  return {
    content: content.trim(),
    truncated,
  }
}

export function buildTaskWithStdin(task: string, stdin: string) {
  if (!stdin) {
    return task
  }

  const instruction = task || 'Explain the following input.'

  return `${instruction}\n\nInput from stdin:\n${stdin}`
}
