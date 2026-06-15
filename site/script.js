const terminalPreview = document.querySelector('.terminal-card pre')
const modeButtons = document.querySelectorAll('.mode-switch button')

const previews = {
  ask: `<code><span class="muted">~/repo</span> ami "explain this project"</code>
<code><span class="tool">read_file</span> ... <span class="tool">search_code</span> ...</code>
<code>Ami reads local files safely, calls small tools, and replies in the terminal.</code>
<code><span class="muted">gpt-4.1-mini · tools 2 · 742 tokens</span></code>`,
  tools: `<code><span class="muted">~/repo</span> ami tools</code>
<code>read_file      list_files      search_code</code>
<code>git_status     web_search      list_tools</code>
<code><span class="muted">local tools stay small and explicit</span></code>`,
  git: `<code><span class="muted">~/repo</span> ami commit</code>
<code><span class="tool">git_status</span> ... <span class="tool">git_diff</span> ...</code>
<code>chore: refine terminal landing page</code>
<code><span class="muted">review, confirm, commit</span></code>`,
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
