package ami

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type Config struct {
	BaseURL   string `json:"baseUrl"`
	APIKey    string `json:"apiKey"`
	Model     string `json:"model"`
	TavilyKey string `json:"tavilyApiKey"`
}

func configPath() string {
	if path := os.Getenv("AMI_CONFIG_FILE"); path != "" {
		return path
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "ami", "config.json")
}
func stateDir() string {
	if v, ok := os.LookupEnv("AMI_STATE_DIR"); ok {
		return v
	}
	base := os.Getenv("XDG_STATE_HOME")
	if base == "" {
		home, _ := os.UserHomeDir()
		base = filepath.Join(home, ".local", "state")
	}
	return filepath.Join(base, "ami")
}
func loadConfig() (Config, error) {
	c := Config{BaseURL: "https://api.deepseek.com/", Model: "deepseek-v4-flash"}
	b, err := os.ReadFile(configPath())
	if err == nil {
		err = json.Unmarshal(b, &c)
		if err == nil && strings.TrimSpace(string(b)) == "null" {
			err = fmt.Errorf("config must be a JSON object")
		}
		if err != nil {
			err = fmt.Errorf("invalid config file %s: %w", configPath(), err)
		}
	} else if os.IsNotExist(err) {
		err = nil
	} else {
		err = fmt.Errorf("cannot read config file: %w", err)
	}
	for k, p := range map[string]*string{"AMI_BASE_URL": &c.BaseURL, "AMI_API_KEY": &c.APIKey, "AMI_MODEL": &c.Model, "AMI_TAVILY_API_KEY": &c.TavilyKey} {
		if v, ok := os.LookupEnv(k); ok {
			*p = v
		}
	}
	return c, err
}
func configValues(c Config) map[string]string {
	return map[string]string{"baseUrl": c.BaseURL, "model": c.Model, "apiKey": c.APIKey, "tavilyApiKey": c.TavilyKey}
}
func maskSecret(v string) string {
	if v == "" {
		return "missing"
	}
	if len(v) <= 12 {
		return strings.Repeat("*", len(v))
	}
	return v[:4] + "****" + v[len(v)-4:]
}
func saveJSON(path string, value any) error {
	b, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	return atomicWrite(path, append(b, '\n'), 0600)
}
func atomicWrite(path string, b []byte, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".ami-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if err = f.Chmod(mode); err == nil {
		_, err = f.Write(b)
	}
	if err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	return os.Rename(f.Name(), path)
}
func trusted() ([]string, error) {
	b, err := os.ReadFile(filepath.Join(stateDir(), "trusted-workspaces.json"))
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var list []string
	err = json.Unmarshal(b, &list)
	return list, err
}
func isTrusted(cwd string) bool {
	list, err := trusted()
	if err != nil {
		return false
	}
	for _, path := range list {
		if path == cwd {
			return true
		}
	}
	return false
}
func setTrust(cwd string, enable bool) error {
	list, err := trusted()
	if err != nil {
		return err
	}
	next := []string{}
	for _, path := range list {
		if path != cwd {
			next = append(next, path)
		}
	}
	if enable {
		next = append(next, cwd)
	}
	return saveJSON(filepath.Join(stateDir(), "trusted-workspaces.json"), next)
}
func canonical(path string) (string, error) {
	p, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	p, err = filepath.EvalSymlinks(p)
	if err != nil {
		return "", err
	}
	info, err := os.Stat(p)
	if err != nil {
		return "", err
	}
	if !info.IsDir() {
		return "", fmt.Errorf("workspace must be a directory")
	}
	return p, nil
}
