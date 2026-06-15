import { gitStatusTool } from './gitStatus'
import { listFilesTool } from './listFiles'
import { listToolsTool } from './listTools'
import { readFileTool } from './readFile'
import { registerTool } from './registry'
import { searchCodeTool } from './searchCode'
import { webSearchTool } from './webSearch'

registerTool(readFileTool)
registerTool(listToolsTool)
registerTool(listFilesTool)
registerTool(searchCodeTool)
registerTool(gitStatusTool)
registerTool(webSearchTool)

export { getTool, listTools } from './registry'
