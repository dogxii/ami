package ami

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

func projectRoot(cwd string) string {
	for directory := cwd; ; directory = filepath.Dir(directory) {
		if _, err := os.Stat(filepath.Join(directory, ".git")); err == nil {
			return directory
		}
		if filepath.Dir(directory) == directory {
			return cwd
		}
	}
}

func loadInstructions(r *Runtime) (string, error) {
	r.instructionRoot = projectRoot(r.Session.Cwd)
	r.instructions = map[string]string{}
	return r.loadInstructionDirs(r.Session.Cwd)
}

func (r *Runtime) loadInstructionDirs(target string) (string, error) {
	directories := []string{}
	for directory := target; ; directory = filepath.Dir(directory) {
		directories = append(directories, directory)
		if directory == r.instructionRoot {
			break
		}
		if filepath.Dir(directory) == directory {
			return "", fmt.Errorf("instruction directory is outside project root")
		}
	}
	parts := []string{}
	loaded := map[string]string{}
	for i := len(directories) - 1; i >= 0; i-- {
		for _, name := range []string{"AGENTS.md", "AMI.md", ".ami/instructions.md"} {
			path, _, err := safePath(r.instructionRoot, filepath.Join(directories[i], name), false)
			if os.IsNotExist(err) {
				continue
			}
			if err != nil {
				return "", err
			}
			info, err := os.Stat(path)
			if err != nil {
				return "", err
			}
			version := fileVersion(info)
			if r.instructions[path] == version {
				continue
			}
			page, err := readBytePage(path, 0, r.Session.Artifacts.PageBytes, false)
			if err != nil {
				return "", err
			}
			content := fmt.Sprintf("Project instructions from %s (more specific directories override parent instructions):\n%s", path, page["content"])
			if page["nextOffset"] != nil {
				id, output, err := r.Session.Artifacts.create()
				if err != nil {
					return "", err
				}
				input, err := os.Open(path)
				if err != nil {
					output.Close()
					return "", err
				}
				_, copyErr := io.Copy(output, input)
				input.Close()
				closeErr := output.Close()
				if copyErr != nil {
					return "", copyErr
				}
				if closeErr != nil {
					return "", closeErr
				}
				content += fmt.Sprintf("\nFull instructions saved: read_output outputId=%s offset=%v before making changes.", id, page["nextOffset"])
			}
			parts = append(parts, content)
			loaded[path] = version
		}
	}
	for path, version := range loaded {
		r.instructions[path] = version
	}
	return strings.Join(parts, "\n\n"), nil
}

func (r *Runtime) checkScopedInstructions(input map[string]any) error {
	if r.instructions == nil {
		return nil
	}
	path, _, err := safePath(r.Session.Cwd, stringArg(input, "path", ""), true)
	if err != nil {
		return err
	}
	content, err := r.loadInstructionDirs(filepath.Dir(path))
	if err != nil {
		return err
	}
	if content != "" {
		return fmt.Errorf("additional project instructions apply; review them before retrying this edit:\n%s", content)
	}
	return nil
}
