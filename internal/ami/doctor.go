package ami

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

type diagnostic struct {
	Name   string `json:"name"`
	Status string `json:"status"`
	Detail string `json:"detail"`
}

func writableDirectory(path string) error {
	if err := os.MkdirAll(path, 0700); err != nil {
		return err
	}
	f, err := os.CreateTemp(path, ".ami-doctor-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	return f.Close()
}

func (app *application) doctor(ctx context.Context, cwd string, a arguments) error {
	checks := []diagnostic{}
	failed := false
	add := func(name, detail string, err error) {
		status := "ok"
		if err != nil {
			status, detail, failed = "error", err.Error(), true
		}
		checks = append(checks, diagnostic{name, status, detail})
	}
	c, configErr := loadConfig()
	secrets := []string{c.APIKey, c.TavilyKey}
	add("config", configPath(), configErr)
	file := map[string]json.RawMessage{}
	if data, err := os.ReadFile(configPath()); err == nil {
		_ = json.Unmarshal(data, &file)
	}
	if model := a.Values["model"]; model != "" {
		c.Model = model
	}
	for _, field := range []struct{ key, env, value string }{
		{"baseUrl", "AMI_BASE_URL", c.BaseURL}, {"model", "AMI_MODEL", c.Model}, {"apiKey", "AMI_API_KEY", maskSecret(c.APIKey)},
	} {
		source := "default"
		if _, ok := file[field.key]; ok {
			source = configPath()
		}
		if _, ok := os.LookupEnv(field.env); ok {
			source = field.env
		}
		if field.key == "model" && a.Values["model"] != "" {
			source = "--model"
		}
		add(field.key, field.value+" ("+source+")", nil)
	}
	endpoint, endpointErr := chatURL(c.BaseURL)
	add("endpoint", endpoint, endpointErr)
	if strings.TrimSpace(c.Model) == "" || c.APIKey == "" {
		add("credentials", "", fmt.Errorf("model and API key are required; run ami init or set AMI_MODEL/AMI_API_KEY"))
	}
	root, err := canonical(cwd)
	add("workspace", root, err)
	for _, path := range []string{filepath.Dir(configPath()), stateDir()} {
		add("writable", path, writableDirectory(path))
	}
	program, _ := shellCommand("")
	for _, name := range []string{program, "git", "rg"} {
		path, lookupErr := exec.LookPath(name)
		if lookupErr != nil && name != program {
			detail := "not installed; Git workflows unavailable"
			if name == "rg" {
				detail = "not installed; search uses the Go fallback"
			}
			checks = append(checks, diagnostic{name, "optional", detail})
		} else {
			add(name, path, lookupErr)
		}
	}
	proxy := []string{}
	for _, name := range []string{"HTTP_PROXY", "HTTPS_PROXY", "ALL_PROXY", "NO_PROXY", "http_proxy", "https_proxy", "all_proxy", "no_proxy"} {
		if os.Getenv(name) != "" {
			proxy = append(proxy, name)
		}
	}
	detail := "no proxy environment variables configured"
	if len(proxy) != 0 {
		detail = "configured: " + strings.Join(proxy, ", ") + "; Go HTTP uses HTTP(S)_PROXY/NO_PROXY, not ALL_PROXY alone"
	}
	add("proxy", detail, nil)
	if a.Bools["check"] && !failed {
		seconds, err := a.number("request-timeout", 30, 1)
		if err != nil || seconds > 3600 {
			return fmt.Errorf("--request-timeout must be between 1 and 3600 seconds")
		}
		p := Provider{Config: c, Timeout: time.Duration(seconds) * time.Second}
		_, err = p.chat(ctx, []Message{message("user", "Reply READY without invoking any tool. This is a connection and tool-schema compatibility check.")}, selectTools(true)[:1], true, nil)
		add("api", "model request and function schema accepted; no tools executed", err)
	}
	for i := range checks {
		checks[i].Detail = redact(checks[i].Detail, secrets).(string)
	}
	if a.Bools["json"] {
		if _, err := fmt.Fprintln(app.out, jsonText(event{"type": "doctor.report", "checks": checks, "ok": !failed})); err != nil {
			return err
		}
	} else {
		for _, check := range checks {
			fmt.Fprintf(app.out, "%s %s: %s\n", check.Status, check.Name, check.Detail)
		}
		if !a.Bools["check"] {
			fmt.Fprintln(app.out, "Use ami doctor --check to send a small model request and verify API/tool-schema compatibility.")
		}
	}
	if failed {
		return &renderedError{fmt.Errorf("diagnostics failed"), ctx.Err() != nil}
	}
	return nil
}
