export const maxToolOutputChars = 12_000

export function truncateToolOutput(
  value: string,
  maxChars = maxToolOutputChars,
) {
  if (value.length <= maxChars) {
    return value
  }

  return `${value.slice(0, maxChars)}\n\n...truncated (${value.length} characters total)`
}
