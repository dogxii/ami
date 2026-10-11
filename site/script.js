const terminalPreview = document.querySelector('.terminal-window pre')
const modeButtons = document.querySelectorAll('.mode-switch button')

const previews = {
  ask: `<code><span class="muted">~/repo</span> ami "what does this project do?"</code>
<code><span class="tool">read_file</span> ... <span class="tool">search_code</span> ...</code>
<code>Ami is a lightweight terminal agent written in Go, distributed as a native executable.
It reads code, edits files, runs commands, and verifies results.</code>
<code><span class="muted">gpt-5.4-mini · 2 tool calls · 684 tokens</span></code>`,
  run: `<code><span class="muted">~/repo</span> ami --yes "fix the failing test"</code>
<code><span class="tool">exec_command</span> · go test ./...</code>
<code><span class="tool">read_file</span> · internal/ami/cli.go</code>
<code><span class="tool">edit_file</span> · internal/ami/cli.go</code>
<code><span class="tool">exec_command</span> · go test ./...</code>
<code>Fixed the parser. All tests passed.</code>`,
  tools: `<code><span class="muted">~/repo</span> ami tools</code>
<code>read_file   - Read a file or line range</code>
<code>list_files  - Explore a directory tree</code>
<code>search_code - Search code with context</code>
<code>git_diff    - Inspect tracked Git changes</code>
<code>edit_file   - Make precise file edits</code>
<code>exec_command - Run and verify commands</code>`,
  git: `<code><span class="muted">~/repo</span> ami commit</code>
<code>Changes
M  site/index.html
M  site/script.js</code>
<code>Commit message
docs: refine landing page preview</code>
<code><span class="muted">Committed 4f8a2c1</span></code>`,
}

modeButtons.forEach((button) => {
  button.addEventListener('click', () => {
    const mode = button.dataset.mode

    if (!mode || !terminalPreview || button.classList.contains('active')) {
      return
    }

    modeButtons.forEach((item) => item.classList.remove('active'))
    button.classList.add('active')
    terminalPreview.classList.add('is-changing')

    window.setTimeout(() => {
      terminalPreview.innerHTML = previews[mode]
      terminalPreview.classList.remove('is-changing')
    }, 180)
  })
})
