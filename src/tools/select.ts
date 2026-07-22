import type { AnyTool } from './type'

const fileTools = ['read_file', 'list_files', 'search_code']
const gitTools = ['git_status', 'git_diff']
const webTools = ['web_search']

const localPathPattern =
  /(?:\.{0,2}\/|~\/|[\w@.-]+\/)[\w@./-]+/
const urlPattern = /https?:\/\/|\bwww\./i
const fileIntentPattern =
  /(?:这个|当前|本地)?(?:项目|仓库|代码库|源码|文件|目录|路径)|这段代码|这个函数|这个类|项目结构|哪里(?:定义|调用|使用)|读取文件|列出目录|搜索代码|查找代码|\b(?:this|current|local)\s+(?:project|repo(?:sitory)?|codebase|file|directory|function|class|module)\b|\b(?:source code|project structure|read (?:the )?file|list (?:the )?(?:files|directory)|search (?:the )?(?:code|repo|project))\b|\bwhere (?:is|are)\b.*\b(?:defined|used|called)\b/i
const specialFilePattern =
  /\b(?:dockerfile|license|makefile|package\.json|readme(?:\.md)?|tsconfig\.json)\b/i
const gitIntentPattern =
  /\b(?:branch|commit|conflict|diff|git|merge|push|rebase|staged|working tree)\b|提交|推送|分支|变更|改动|暂存|工作区|冲突|合并|远端/i
const webIntentPattern =
  /https?:\/\/|\bwww\.|\blatest\b.{0,30}\b(?:news|price|release|version)\b|\b(?:news|official (?:docs?|documentation|website)|online search|search (?:the )?web|weather|web search)\b|联网|上网|网络搜索|网页搜索|搜索引擎|搜索功能|自带搜索|最新.{0,20}(?:消息|新闻|版本|发布|价格)|新闻|天气|官网|官方文档/i
const casualPattern =
  /^(?:bye|goodbye|hello|hey|hi|ok(?:ay)?|thanks?|thank you|你好(?:呀|啊)?|您好|嗨|哈喽|谢谢|感谢|好的?|好吧|懂了|明白了|再见)[!！,.，。?？\s]*$/i
const generalQuestionPattern =
  /\b(?:difference between|how (?:do|does|to)|what (?:is|are)|which is better|why (?:do|does|is))\b|是什么|什么意思|为什么|怎么用|如何使用|有什么区别|区别是什么|哪个好|推荐哪个|对比一下/i

export function selectTools(
  task: string,
  tools: AnyTool[],
  options: { hasStdin?: boolean } = {},
) {
  const selectedNames = new Set<string>()
  const hasWebIntent = webIntentPattern.test(task)

  if (
    (localPathPattern.test(task) && !urlPattern.test(task)) ||
    fileIntentPattern.test(task) ||
    specialFilePattern.test(task)
  ) {
    addTools(selectedNames, fileTools)
  }

  if (gitIntentPattern.test(task)) {
    addTools(selectedNames, gitTools)
  }

  if (hasWebIntent) {
    addTools(selectedNames, webTools)
  }

  if (selectedNames.size > 0) {
    return tools.filter((tool) => selectedNames.has(tool.name))
  }

  if (
    !task.trim() ||
    options.hasStdin ||
    casualPattern.test(task.trim()) ||
    generalQuestionPattern.test(task)
  ) {
    return []
  }

  return tools
}

function addTools(target: Set<string>, names: string[]) {
  for (const name of names) {
    target.add(name)
  }
}
