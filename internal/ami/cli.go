package ami

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"os"
	"os/signal"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"

	"golang.org/x/term"
)

const help = `ami — lightweight native terminal agent

Usage: ami [options] <task>
       ami <command> [options]

Commands:
  init                    Create global configuration
  config [get|set] [key]   Show or edit configuration (secrets masked)
  tools                   List all 11 tools
  tool <name> [json]       Run one explicitly authorized tool
  sessions                List saved workspace sessions
  output <id>             Read saved output (--session, --offset)
  trust                   Trust workspace (--yes, --list, --revoke)
  commit                  Generate and confirm a commit (--all, --yes)
  push                    Push commits (--commit, --rebase, --yes)
  update                  Install latest native release (--yes)

Options:
  --cwd <path>            Workspace directory
  --model <model>         Override configured model
  --yes                   Allow edits and host shell commands
  --ask                   Ask even in a trusted workspace
  --read-only             Block file writes and shell commands
  --continue              Resume latest workspace task
  --session <id>          Resume a specific session
  --no-session            Skip history persistence; retain temporary outputs
  --json                  Emit JSONL events on stdout
  --max-steps <count>      Tool budget (0 = unlimited)
  --max-tokens <count>     Token budget (0 = unlimited)
  --timeout <seconds>      Task deadline (0 = unlimited)
  --context-window <n>     Model capacity (default 32000)
  --output-bytes <n>       Content page budget (default 16000)
  --request-timeout <s>    Model request deadline (default 180)
  --debug                 Include redacted provider error details
  -h, --help              Show help
  -v, --version           Show version
`

type arguments struct {
	Values map[string]string
	Bools  map[string]bool
	Pos    []string
}

func parseArgs(args []string) (arguments, error) {
	a := arguments{Values: map[string]string{}, Bools: map[string]bool{}}
	bools := map[string]bool{}
	for _, k := range strings.Fields("yes ask read-only continue no-session json debug list revoke all commit rebase help version") {
		bools[k] = true
	}
	values := map[string]bool{}
	for _, k := range strings.Fields("cwd model session max-steps max-tokens timeout context-window output-bytes request-timeout offset") {
		values[k] = true
	}
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--" {
			a.Pos = append(a.Pos, args[i+1:]...)
			break
		}
		if arg == "-h" {
			arg = "--help"
		}
		if arg == "-v" {
			arg = "--version"
		}
		if !strings.HasPrefix(arg, "-") {
			a.Pos = append(a.Pos, arg)
			continue
		}
		if !strings.HasPrefix(arg, "--") {
			return a, fmt.Errorf("unknown option: %s", arg)
		}
		key, value, equals := strings.Cut(arg[2:], "=")
		if bools[key] {
			enabled := true
			if equals {
				var err error
				enabled, err = strconv.ParseBool(value)
				if err != nil {
					return a, fmt.Errorf("--%s expects true or false", key)
				}
			}
			a.Bools[key] = enabled
		} else if values[key] {
			if !equals {
				if i+1 >= len(args) || strings.HasPrefix(args[i+1], "--") {
					return a, fmt.Errorf("--%s requires a value", key)
				}
				i++
				value = args[i]
			}
			if value == "" {
				return a, fmt.Errorf("--%s requires a value", key)
			}
			a.Values[key] = value
		} else {
			return a, fmt.Errorf("unknown option: --%s", key)
		}
	}
	return a, nil
}
func (a arguments) number(k string, def, minimum int) (int, error) {
	v, ok := a.Values[k]
	if !ok {
		return def, nil
	}
	n, err := strconv.ParseInt(v, 10, 64)
	if err != nil || n < int64(minimum) || n > int64(math.MaxInt) {
		return 0, fmt.Errorf("--%s must be an integer >= %d", k, minimum)
	}
	return int(n), nil
}

type application struct {
	in       *os.File
	out, err io.Writer
	version  string
	reader   *bufio.Reader
}

func Main(args []string, version string) int {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	app := application{in: os.Stdin, out: os.Stdout, err: os.Stderr, version: version, reader: bufio.NewReader(os.Stdin)}
	a, err := parseArgs(args)
	if err == nil {
		err = app.run(ctx, a)
	}
	if err == nil {
		return 0
	}
	if rendered, ok := err.(*renderedError); ok {
		if rendered.cancelled {
			return 130
		}
		return 1
	}
	code := 1
	if ctx.Err() != nil || err == context.Canceled || err == context.DeadlineExceeded {
		code = 130
	}
	if a.Bools["json"] {
		kind := "run.failed"
		if code == 130 {
			kind = "run.cancelled"
		}
		fmt.Fprintln(app.out, jsonText(event{"type": kind, "timestamp": time.Now().UTC().Format(time.RFC3339Nano), "error": redact(err.Error(), []string{loadConfig().APIKey, loadConfig().TavilyKey})}))
	} else {
		fmt.Fprintln(app.err, redact(err.Error(), []string{loadConfig().APIKey, loadConfig().TavilyKey}))
	}
	return code
}
func (app *application) tty() bool { return term.IsTerminal(int(app.in.Fd())) }
func (app *application) prompt(ctx context.Context, label, def string, secret bool) (string, error) {
	if !app.tty() {
		return "", fmt.Errorf("interactive input requires a terminal")
	}
	fmt.Fprintf(app.err, "%s", label)
	if def != "" {
		fmt.Fprintf(app.err, " [%s]", def)
	}
	fmt.Fprint(app.err, ": ")
	if secret {
		state, err := term.GetState(int(app.in.Fd()))
		if err != nil {
			return "", err
		}
		defer term.Restore(int(app.in.Fd()), state)
	}
	type result struct {
		s   string
		err error
	}
	ch := make(chan result, 1)
	go func() {
		if secret {
			b, err := term.ReadPassword(int(app.in.Fd()))
			fmt.Fprintln(app.err)
			ch <- result{string(b), err}
		} else {
			s, err := app.reader.ReadString('\n')
			ch <- result{s, err}
		}
	}()
	select {
	case <-ctx.Done():
		return "", ctx.Err()
	case res := <-ch:
		if res.err != nil {
			return "", res.err
		}
		s := strings.TrimSpace(res.s)
		if s == "" {
			s = def
		}
		return s, nil
	}
}
func (app *application) confirm(ctx context.Context, label string) (bool, error) {
	s, err := app.prompt(ctx, label+" [y/N]", "", false)
	return strings.EqualFold(s, "y") || strings.EqualFold(s, "yes"), err
}
func (app *application) run(ctx context.Context, a arguments) error {
	if a.Bools["help"] {
		fmt.Fprint(app.out, help)
		return nil
	}
	if a.Bools["version"] {
		fmt.Fprintln(app.out, "ami/"+app.version)
		return nil
	}
	cwd := a.Values["cwd"]
	if cwd == "" {
		cwd = "."
	}
	command := ""
	if len(a.Pos) > 0 {
		command = a.Pos[0]
	}
	allowedOptions := "cwd model yes ask read-only continue session no-session json max-steps max-tokens timeout context-window output-bytes request-timeout debug"
	commandOptions := map[string]string{
		"tools": "", "config": "", "init": "", "sessions": "cwd", "output": "cwd session offset",
		"trust": "cwd yes list revoke", "tool": "cwd read-only", "commit": "cwd all yes", "push": "cwd commit rebase yes", "update": "yes",
	}
	if options, known := commandOptions[command]; known {
		allowedOptions = options
		if command != "tool" && command != "output" && command != "config" && len(a.Pos) > 1 {
			return fmt.Errorf("unexpected arguments for ami %s", command)
		}
	}
	allowed := map[string]bool{}
	for _, key := range strings.Fields(allowedOptions) {
		allowed[key] = true
	}
	for key := range a.Values {
		if !allowed[key] {
			return fmt.Errorf("option --%s is unavailable for this command", key)
		}
	}
	for key := range a.Bools {
		if !allowed[key] {
			return fmt.Errorf("option --%s is unavailable for this command", key)
		}
	}
	switch command {
	case "tools":
		for _, tool := range toolCatalog {
			fmt.Fprintf(app.out, "%s [%s] - %s\n", tool.Name, tool.Capability, tool.Description)
		}
		return nil
	case "config":
		return app.config(a.Pos[1:])
	case "init":
		return app.init(ctx)
	case "trust":
		return app.trust(ctx, cwd, a)
	case "sessions":
		return app.sessions(cwd)
	case "output":
		return app.output(cwd, a)
	case "tool":
		return app.tool(ctx, cwd, a)
	case "commit":
		return app.commit(ctx, cwd, a)
	case "push":
		return app.push(ctx, cwd, a)
	case "update":
		return app.update(ctx, a)
	default:
		return app.task(ctx, cwd, a)
	}
}
func (app *application) config(args []string) error {
	values := configValues(loadConfig())
	if len(args) == 0 {
		for _, key := range []string{"model", "baseUrl", "apiKey", "tavilyApiKey"} {
			value := values[key]
			if strings.HasSuffix(key, "Key") {
				value = maskSecret(value)
			}
			fmt.Fprintf(app.out, "%s: %s\n", key, value)
		}
		fmt.Fprintln(app.out, "\nconfig saved in "+configPath())
		return nil
	}
	if len(args) < 2 {
		return fmt.Errorf("usage: ami config get <key> | set <key> <value>")
	}
	key := args[1]
	if _, ok := values[key]; !ok {
		return fmt.Errorf("unknown config key: %s", key)
	}
	switch args[0] {
	case "get":
		v := values[key]
		if strings.HasSuffix(key, "Key") {
			v = maskSecret(v)
		}
		fmt.Fprintln(app.out, v)
		return nil
	case "set":
		if len(args) != 3 || strings.TrimSpace(args[2]) == "" {
			return fmt.Errorf("usage: ami config set <key> <value>")
		}
		file := map[string]string{}
		b, err := os.ReadFile(configPath())
		if err == nil {
			if err = json.Unmarshal(b, &file); err != nil {
				return fmt.Errorf("invalid config file: %w", err)
			}
		} else if !os.IsNotExist(err) {
			return err
		}
		file[key] = args[2]
		if err = saveJSON(configPath(), file); err != nil {
			return err
		}
		fmt.Fprintln(app.out, "Updated "+key)
		return nil
	default:
		return fmt.Errorf("unknown config action: %s", args[0])
	}
}
func (app *application) init(ctx context.Context) error {
	if _, err := os.Stat(configPath()); err == nil {
		return fmt.Errorf("config.json already exists; use ami config set")
	}
	provider, err := app.prompt(ctx, "Provider (deepseek/openai/qwen/custom)", "deepseek", false)
	if err != nil {
		return err
	}
	c := Config{}
	switch provider {
	case "deepseek":
		c.BaseURL = "https://api.deepseek.com/"
		c.Model = "deepseek-v4-flash"
	case "openai":
		c.BaseURL = "https://api.openai.com/"
		c.Model = "gpt-5.4-mini"
	case "qwen":
		c.BaseURL = "https://dashscope.aliyuncs.com/compatible-mode/"
		c.Model = "qwen-plus"
	case "custom":
	default:
		return fmt.Errorf("unknown provider: %s", provider)
	}
	if c.BaseURL, err = app.prompt(ctx, "Base URL", c.BaseURL, false); err != nil {
		return err
	}
	if _, err = chatURL(c.BaseURL); err != nil {
		return err
	}
	if c.Model, err = app.prompt(ctx, "Model", c.Model, false); err != nil {
		return err
	}
	if c.APIKey, err = app.prompt(ctx, "API key", "", true); err != nil {
		return err
	}
	if c.Model == "" || c.APIKey == "" {
		return fmt.Errorf("model and API key are required")
	}
	if c.TavilyKey, err = app.prompt(ctx, "Tavily API key (optional)", "", true); err != nil {
		return err
	}
	b, _ := json.MarshalIndent(c, "", "  ")
	if err = os.MkdirAll(filepath.Dir(configPath()), 0700); err != nil {
		return err
	}
	f, err := os.OpenFile(configPath(), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	_, err = f.Write(append(b, '\n'))
	ce := f.Close()
	if err != nil {
		return err
	}
	fmt.Fprintln(app.out, "Created "+configPath())
	return ce
}
func (app *application) trust(ctx context.Context, cwd string, a arguments) error {
	if a.Bools["list"] {
		list, err := trusted()
		if err != nil {
			return err
		}
		if len(list) == 0 {
			fmt.Fprintln(app.out, "No trusted workspaces.")
		} else {
			fmt.Fprintln(app.out, strings.Join(list, "\n"))
		}
		return nil
	}
	root, err := canonical(cwd)
	if err != nil {
		return err
	}
	if !a.Bools["revoke"] && !a.Bools["yes"] {
		yes, err := app.confirm(ctx, "Trust "+root+"? Ami may edit files and run commands with your account permissions.")
		if err != nil {
			return fmt.Errorf("use ami trust --yes to explicitly authorize this workspace")
		}
		if !yes {
			return nil
		}
	}
	if err = setTrust(root, !a.Bools["revoke"]); err != nil {
		return err
	}
	label := "Trusted: "
	if a.Bools["revoke"] {
		label = "Revoked trust: "
	}
	fmt.Fprintln(app.out, label+root)
	return nil
}
func (app *application) sessions(cwd string) error {
	root, err := canonical(cwd)
	if err != nil {
		return err
	}
	entries, err := os.ReadDir(filepath.Join(stateDir(), "sessions"))
	if os.IsNotExist(err) {
		entries = nil
	} else if err != nil {
		return err
	}
	type row struct{ id, timestamp string }
	rows := []row{}
	for _, entry := range entries {
		if !entry.IsDir() || !validID.MatchString(entry.Name()) {
			continue
		}
		header, err := sessionHeader(root, entry.Name())
		if err != nil {
			continue
		}
		timestamp, _ := header["timestamp"].(string)
		rows = append(rows, row{entry.Name(), timestamp})
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].timestamp > rows[j].timestamp })
	if len(rows) == 0 {
		fmt.Fprintln(app.out, "No saved sessions for this workspace.")
	}
	for _, row := range rows {
		fmt.Fprintf(app.out, "%s  %s\n", row.id, row.timestamp)
	}
	return nil
}
func (app *application) output(cwd string, a arguments) error {
	if len(a.Pos) != 2 {
		return fmt.Errorf("usage: ami output <outputId>")
	}
	root, err := canonical(cwd)
	if err != nil {
		return err
	}
	id := a.Values["session"]
	if id == "" {
		id = "latest"
	}
	id, err = sessionID(root, id)
	if err != nil {
		return err
	}
	if _, err = sessionHeader(root, id); err != nil {
		return err
	}
	offset, err := a.number("offset", 0, 0)
	if err != nil {
		return err
	}
	store := &Artifacts{Directory: filepath.Join(stateDir(), "sessions", id, "outputs"), PageBytes: 16000}
	page, err := store.page(a.Pos[1], int64(offset), 0)
	if err != nil {
		return err
	}
	if page["encoding"] == "base64" {
		fmt.Fprintln(app.err, "Binary output page (base64):")
	}
	fmt.Fprint(app.out, page["content"])
	if page["nextOffset"] != nil {
		fmt.Fprintf(app.err, "\nContinue: ami output %s --session %s --offset %v\n", a.Pos[1], id, page["nextOffset"])
	}
	return nil
}
func (app *application) tool(ctx context.Context, cwd string, a arguments) error {
	if len(a.Pos) < 2 || len(a.Pos) > 3 {
		return fmt.Errorf("usage: ami tool <name> [json]")
	}
	input := map[string]any{}
	if len(a.Pos) == 3 {
		if err := json.Unmarshal([]byte(a.Pos[2]), &input); err != nil {
			return err
		}
	}
	mode := "auto"
	if a.Bools["read-only"] {
		mode = "read-only"
	}
	r, err := newRuntime(ctx, cwd, "", true, 16000, mode, nil, nil, nil)
	if err != nil {
		return err
	}
	defer r.Session.close()
	defer r.cleanup()
	result, err := executeTool(r, a.Pos[1], input, toolAllowlist(toolCatalog))
	if err != nil {
		return err
	}
	fmt.Fprintln(app.out, result)
	fmt.Fprintln(app.err, "Outputs saved in session "+r.Session.ID)
	return nil
}
func (app *application) task(ctx context.Context, cwd string, a arguments) error {
	if len(a.Pos) == 0 && !a.Bools["continue"] && a.Values["session"] == "" && app.tty() && !a.Bools["json"] {
		fmt.Fprint(app.out, help)
		return nil
	}
	modes := 0
	mode := ""
	for _, pair := range [][2]string{{"yes", "auto"}, {"ask", "ask"}, {"read-only", "read-only"}} {
		if a.Bools[pair[0]] {
			modes++
			mode = pair[1]
		}
	}
	if modes > 1 {
		return fmt.Errorf("choose one of --yes, --ask or --read-only")
	}
	resume := a.Values["session"]
	if a.Bools["continue"] {
		if resume != "" {
			return fmt.Errorf("choose --continue or --session")
		}
		resume = "latest"
	}
	window, err := a.number("context-window", 32000, 8192)
	if err != nil {
		return err
	}
	page, err := a.number("output-bytes", 16000, 256)
	if err != nil {
		return err
	}
	page = min(page, window-window/5)
	steps, err := a.number("max-steps", 0, 0)
	if err != nil {
		return err
	}
	tokens, err := a.number("max-tokens", 0, 0)
	if err != nil {
		return err
	}
	timeout, err := a.number("timeout", 0, 0)
	if err != nil {
		return err
	}
	requestTimeout, err := a.number("request-timeout", 180, 1)
	if err != nil {
		return err
	}
	if timeout > int(math.MaxInt64/int64(time.Second)) || requestTimeout > int(math.MaxInt64/int64(time.Second)) {
		return fmt.Errorf("timeout is too large")
	}
	if timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, time.Duration(timeout)*time.Second)
		defer cancel()
	}
	config := loadConfig()
	if v := a.Values["model"]; v != "" {
		config.Model = v
	}
	render := &renderer{out: app.out, err: app.err, json: a.Bools["json"]}
	var confirm func(context.Context, string) (bool, error)
	if app.tty() && !a.Bools["json"] {
		confirm = app.confirm
	}
	r, err := newRuntime(ctx, cwd, resume, !a.Bools["no-session"], page, mode, render.render, confirm, []string{config.APIKey, config.TavilyKey})
	if err != nil {
		return err
	}
	defer r.Session.close()
	defer r.cleanup()
	task := strings.Join(a.Pos, " ")
	if !app.tty() {
		var input string
		input, err = app.stdin(ctx, r.Session.Artifacts)
		if err != nil {
			return err
		}
		if input != "" {
			if task == "" {
				task = "Explain the following input."
			}
			task += "\n\n" + input
		}
	}
	if task == "" && resume != "" {
		task = "Continue the previous task, checking current state first."
	}
	if task == "" {
		return fmt.Errorf("provide a task or pipe input to ami")
	}
	var notice <-chan string
	if app.tty() && !a.Bools["json"] && os.Getenv("AMI_NO_UPDATE_CHECK") != "1" {
		var cancelNotice context.CancelFunc
		notice, cancelNotice = updateNotice(ctx, app.version)
		defer cancelNotice()
	}
	outcome := runAgent(r, AgentOptions{Task: task, Provider: &Provider{Config: config, Timeout: time.Duration(requestTimeout) * time.Second}, ContextWindow: window, MaxSteps: steps, MaxTokens: tokens, Debug: a.Bools["debug"]})
	if outcome.Err != nil {
		return &renderedError{outcome.Err, outcome.Status == "cancelled"}
	}
	select {
	case version := <-notice:
		fmt.Fprintf(app.err, "Update available: %s -> %s. Run ami update\n", app.version, version)
	default:
	}
	return nil
}

type renderedError struct {
	error
	cancelled bool
}

func (app *application) stdin(ctx context.Context, a *Artifacts) (string, error) {
	id, f, err := a.create()
	if err != nil {
		return "", err
	}
	defer f.Close()
	done := make(chan struct{})
	defer close(done)
	go func() {
		select {
		case <-ctx.Done():
			app.in.Close()
		case <-done:
		}
	}()
	if _, err = io.Copy(f, app.in); err != nil {
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		return "", err
	}
	if err = f.Close(); err != nil {
		return "", err
	}
	page, err := a.page(id, 0, a.PageBytes/2)
	if err != nil {
		return "", err
	}
	content := strings.TrimSpace(page["content"].(string))
	if content == "" && page["nextOffset"] == nil {
		return "", nil
	}
	label := "Input from stdin"
	if page["encoding"] == "base64" {
		label += " (base64 binary page)"
	}
	result := label + ":\n" + content
	if page["nextOffset"] != nil {
		result += fmt.Sprintf("\n\nComplete input saved. Use read_output with outputId=%s, offset=%v to continue.", id, page["nextOffset"])
	}
	return result, nil
}
