import { gitDiffTool } from './gitDiff'
import { gitStatusTool } from './gitStatus'
import { listFilesTool } from './listFiles'
import { readFileTool } from './readFile'
import { registerTool } from './registry'
import { searchCodeTool } from './searchCode'
import { webSearchTool } from './webSearch'

registerTool(readFileTool)
registerTool(listFilesTool)
registerTool(searchCodeTool)
registerTool(gitStatusTool)
registerTool(gitDiffTool)
registerTool(webSearchTool)

export { getTool, listTools } from './registry'
