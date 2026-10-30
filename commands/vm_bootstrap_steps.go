package commands

import (
	"bytes"
	"embed"
	"fmt"
	"slices"
	"strings"
	"text/template"
	"unicode"

	"golang.org/x/text/unicode/norm"
)

//go:embed bootstrap/*.tmpl
var bootstrapScripts embed.FS
var bootstrapTemplates = template.Must(template.ParseFS(bootstrapScripts, "bootstrap/*.tmpl"))

type bootstrapVariable struct{ name, value string }
type bootstrapEnvironment []bootstrapVariable

func (e bootstrapEnvironment) has(key string) bool {
	for _, v := range e {
		if v.name == key {
			return true
		}
	}
	return false
}
func (e *bootstrapEnvironment) set(key, value string) {
	for i := range *e {
		if (*e)[i].name == key {
			(*e)[i].value = value
			return
		}
	}
	*e = append(*e, bootstrapVariable{key, value})
}
func bootstrapIdentifier(name string) bool {
	if name == "" {
		return false
	}
	start := func(r rune) bool {
		return r == '_' || unicode.IsLetter(r) || unicode.Is(unicode.Nl, r) || unicode.Is(unicode.Other_ID_Start, r)
	}
	more := func(r rune) bool {
		return start(r) || unicode.In(r, unicode.Mn, unicode.Mc, unicode.Nd, unicode.Pc, unicode.Other_ID_Continue)
	}
	for i, r := range name {
		if (i == 0 && !start(r)) || (i > 0 && !more(r)) {
			return false
		}
		// Python uses XID: normalization must keep the identifier's shape.
		for j, n := range norm.NFKC.String(string(r)) {
			if i == 0 && j == 0 && !start(n) || (i != 0 || j != 0) && !more(n) {
				return false
			}
		}
	}
	return true
}
func parseBootstrapEnv(d Dependencies, flags []string) (bootstrapEnvironment, error) {
	env := bootstrapEnvironment{}
	for _, flag := range flags {
		key, value, explicit := strings.Cut(flag, "=")
		if !explicit {
			var ok bool
			value, ok = d.LookupEnv(key)
			if !ok {
				return nil, &ExitError{2, "Error: Invalid value for --env: " + pythonQuote(key) + " is not set in the local environment"}
			}
		}
		key = strings.TrimSpace(key)
		if !bootstrapIdentifier(key) {
			return nil, &ExitError{2, "Error: Invalid value for --env: " + pythonQuote(key) + " is not a valid variable name"}
		}
		if strings.Contains(value, "\n") {
			return nil, &ExitError{2, "Error: Invalid value for --env: " + key + ": values containing newlines are not supported"}
		}
		env.set(key, value)
	}
	return env, nil
}
func parseBootstrapAgents(flags []string) ([]string, error) {
	agents := []string{}
	for _, flag := range flags {
		for _, value := range strings.Split(flag, ",") {
			agent := strings.ToLower(strings.TrimSpace(value))
			if agent == "" {
				continue
			}
			if agent != "claude" && agent != "codex" {
				return nil, &ExitError{2, "Error: Invalid value for --install: unknown agent " + pythonQuote(agent) + " (choose from: claude, codex)"}
			}
			if !slices.Contains(agents, agent) {
				agents = append(agents, agent)
			}
		}
	}
	return agents, nil
}
func bootstrapCredentialKeys(agent string) []string {
	switch agent {
	case "claude":
		return []string{"ANTHROPIC_API_KEY", "CLAUDE_CODE_OAUTH_TOKEN"}
	case "codex":
		return []string{"OPENAI_API_KEY"}
	default:
		panic("unvalidated bootstrap agent")
	}
}

type bootstrapStep struct {
	name, description, script, stdin string
	secret                           bool
}
type bootstrapOptions struct {
	org, identity, repo, ref, dotfiles, run string
	installs, environment                   []string
	github, forward, installAVR, print      bool
}

func bootstrapScript(name string, fields map[string]any) (string, error) {
	var output bytes.Buffer
	err := bootstrapTemplates.ExecuteTemplate(&output, name+".sh.tmpl", fields)
	return output.String(), err
}
func bootstrapSteps(o bootstrapOptions, env bootstrapEnvironment, agents []string, token string, runScript *string) ([]bootstrapStep, error) {
	steps := []bootstrapStep{}
	add := func(name, description, scriptName string, fields map[string]any, stdin string, secret bool) error {
		script, err := bootstrapScript(scriptName, fields)
		if err != nil {
			return err
		}
		if o.print && secret {
			stdin = ""
		}
		steps = append(steps, bootstrapStep{name, description, script, stdin, secret})
		return nil
	}
	if len(env) > 0 {
		keys := []string{}
		var blob strings.Builder
		for _, v := range env {
			keys = append(keys, v.name)
			fmt.Fprintf(&blob, "export %s=%s\n", v.name, shellQuotePython(v.value))
		}
		if err := add("env", fmt.Sprintf("Set %d environment variable(s): %s", len(env), strings.Join(keys, ", ")), "env", map[string]any{"Count": len(env)}, blob.String(), true); err != nil {
			return nil, err
		}
	}
	if o.github {
		if err := add("github", "Authenticate the VM's GitHub CLI and git with your local token", "github", nil, token, true); err != nil {
			return nil, err
		}
	}
	if o.repo != "" {
		label := "Clone " + o.repo
		if o.ref != "" {
			label += " @ " + o.ref
		}
		if err := add("repo", label, "repo", map[string]any{"URL": shellQuotePython(o.repo), "Ref": shellQuotePython(o.ref)}, "", false); err != nil {
			return nil, err
		}
	}
	if o.dotfiles != "" {
		if err := add("dotfiles", "Install dotfiles from "+o.dotfiles, "dotfiles", map[string]any{"URL": shellQuotePython(o.dotfiles)}, "", false); err != nil {
			return nil, err
		}
	}
	if len(agents) > 0 {
		packages := []string{}
		for _, agent := range agents {
			pkg := "@openai/codex"
			if agent == "claude" {
				pkg = "@anthropic-ai/claude-code"
			}
			packages = append(packages, shellQuotePython(pkg))
		}
		if err := add("install-agents", "Install "+strings.Join(agents, ", ")+" (npm -g)", "install_agents", map[string]any{"Packages": strings.Join(packages, " "), "Agents": strings.Join(agents, ", ")}, "", false); err != nil {
			return nil, err
		}
	}
	if o.installAVR {
		if err := add("install-avr", "Install the avr CLI", "install_avr", nil, "", false); err != nil {
			return nil, err
		}
	}
	if runScript != nil {
		steps = append(steps, bootstrapStep{name: "run", description: "Run custom script", script: *runScript})
	}
	return steps, nil
}
