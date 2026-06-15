# Ami Agent

Ami is a lightweight terminal AI agent built with Bun and TypeScript.

It runs small natural-language tasks from the command line, can call local tools,
and keeps the output compact enough for daily terminal use.

## Features

- Natural-language CLI tasks with `ami <task>`
- OpenAI-compatible chat completions
- Streaming final answers
- Extensible local tool registry
- Safe file reading with cwd and size checks
- Code search, file listing, git status, and web search tools
- Global config stored at `~/.config/ami/config.json`
- AI-assisted `ami commit` and guarded `ami push`

## Requirements

- Bun 1.0+
- An OpenAI-compatible API key
- Optional Tavily API key for `web_search`

## Install

From npm:

```bash
npm install -g @dogxi/ami
```

For local development:

```bash
bun install
bun link
```

Then run:

```bash
ami init
```

## Usage

Ask a question from any project directory:

```bash
ami "explain src/cli.ts"
ami "summarize the git status"
ami "search where loadConfig is used"
```

List available tools:

```bash
ami tools
```

Run a tool directly:

```bash
ami tool read_file '{"path":"src/cli.ts"}'
```

Generate a commit message from staged changes:

```bash
ami commit
```

Stage all changes before generating the commit message:

```bash
ami commit --all
```

Push committed changes after branch checks:

```bash
ami push
```

## Config

Create the global config interactively:

```bash
ami init
```

Show current config:

```bash
ami config
```

Set a config value:

```bash
ami config set model deepseek-chat
ami config set apiKey sk-...
ami config set tavilyApiKey tvly-...
```

Environment variables override the config file:

```bash
AMI_BASE_URL=https://api.deepseek.com/
AMI_API_KEY=...
AMI_MODEL=deepseek-chat
AMI_TAVILY_API_KEY=...
```

## Development

```bash
bun run dev "explain package.json"
bun typecheck
```

## Safety Notes

- `read_file` only reads files inside the current working directory.
- `.env`, `.git`, and `node_modules` paths are blocked from file reads.
- Large files are rejected by the file reading tool.
- `ami commit` asks for confirmation before committing.
- `ami push` checks branch status and asks for confirmation unless `--yes` is used.
