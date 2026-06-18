const terminalPreview = document.querySelector('.terminal-window pre')
const modeButtons = document.querySelectorAll('.mode-switch button')

const previews = {
  ask: `<code><span class="muted">~/repo</span> ami "what does this project do?"</code>
<code><span class="tool">read_file</span> ... <span class="tool">search_code</span> ...</code>
<code>Ami is a lightweight terminal agent built with Bun and TypeScript.
It supports natural-language tasks, tool calls, streaming output, and Git helpers.</code>
<code><span class="muted">gpt-5.4-mini · read_file x1, search_code x1 · 684 tokens</span></code>`,
  tools: `<code><span class="muted">~/repo</span> ami tools</code>
<code>read_file   - Read a local text file</code>
<code>list_files  - List files in a directory</code>
<code>web_search  - Search recent or external information</code>
<code>...</code>`,
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
