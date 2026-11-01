package commands

import (
	"fmt"
	"slices"
	"strings"

	"github.com/avrea-com/cli/output"
	"github.com/spf13/cobra"
)

func newVMBootstrap(d Dependencies) *cobra.Command {
	o := bootstrapOptions{}
	c := &cobra.Command{Use: "bootstrap VM_ID", Short: "Set up a running VM with development tools over SSH.", Args: positionalArguments("VM_ID")}
	orgFlag(c, &o.org)
	f := c.Flags()
	f.StringVarP(&o.identity, "identity", "i", "", "SSH private key file.")
	f.BoolVar(&o.github, "setup-github", false, "Forward your local GitHub CLI token to the VM over SSH stdin.")
	f.Var(inverseBool{&o.github}, "no-setup-github", "Skip GitHub authentication setup.")
	f.Lookup("no-setup-github").NoOptDefVal = "true"
	f.StringArrayVar(&o.installs, "install", nil, "Agent CLIs: claude, codex; repeatable or comma-separated.")
	f.BoolVar(&o.forward, "forward-agent-creds", false, "Forward agent credentials from the local environment.")
	f.BoolVar(&o.installAVR, "install-avr", false, "Install the avr CLI in the VM.")
	f.StringVar(&o.repo, "repo", "", "Repository URL to clone into the VM's home directory.")
	f.StringVar(&o.ref, "ref", "", "Branch to check out; requires --repo.")
	f.StringVar(&o.dotfiles, "dotfiles", "", "Dotfiles repository to clone and install.")
	f.StringArrayVar(&o.environment, "env", nil, "KEY=VALUE or a local environment variable name; repeatable.")
	f.StringVar(&o.run, "run", "", "Custom script to run last, inline or @path.")
	f.BoolVar(&o.print, "print", false, "Print the ordered plan with secret input redacted.")
	c.RunE = run(func(cmd *cobra.Command, args []string) error {
		if o.ref != "" && o.repo == "" {
			return &ExitError{2, "Error: --ref requires --repo."}
		}
		if o.ref != "" {
			value, err := validateVMBranch(o.ref)
			if err != nil {
				return err
			}
			o.ref = value
		}
		agents, err := parseBootstrapAgents(o.installs)
		if err != nil {
			return err
		}
		env, err := parseBootstrapEnv(d, o.environment)
		if err != nil {
			return err
		}
		if o.forward {
			selected := agents
			if len(selected) == 0 {
				selected = []string{"claude", "codex"}
			}
			for _, agent := range selected {
				for _, key := range bootstrapCredentialKeys(agent) {
					if value := d.Getenv(key); value != "" && !env.has(key) {
						env.set(key, value)
					}
				}
			}
			if env.has("ANTHROPIC_API_KEY") && env.has("CLAUDE_CODE_OAUTH_TOKEN") {
				if _, err = fmt.Fprintln(d.Err, "Warning: both ANTHROPIC_API_KEY and CLAUDE_CODE_OAUTH_TOKEN are set; Claude Code prefers the API key, so your subscription token won't be used. Unset one to choose."); err != nil {
					return err
				}
			}
			if !o.print && slices.Contains(agents, "claude") && !env.has("ANTHROPIC_API_KEY") && !env.has("CLAUDE_CODE_OAUTH_TOKEN") {
				if err = bootstrapClaudePrompt(cmd.Context(), d, &env); err != nil {
					return err
				}
			}
			for _, agent := range agents {
				keys := bootstrapCredentialKeys(agent)
				found := false
				for _, key := range keys {
					found = found || env.has(key)
				}
				if !found {
					if _, err = fmt.Fprintf(d.Err, "Warning: no credential (%s) is set locally, so %s will be unauthenticated on the VM.\n", strings.Join(keys, ", "), agent); err != nil {
						return err
					}
				}
			}
			if !o.print && slices.Contains(agents, "claude") && env.has("CLAUDE_CODE_OAUTH_TOKEN") {
				if _, err = fmt.Fprintln(d.Err, "Note: the token authenticates claude for headless use (`claude -p` and the SDK). The interactive `claude` TUI still shows its onboarding login screen — the env token does not skip it; that is expected."); err != nil {
					return err
				}
			}
		}
		var script *string
		if f.Changed("run") {
			value := o.run
			if strings.HasPrefix(value, "@") {
				value, _, err = vmInputFile(value[1:], "--run", "run script")
				if err != nil {
					return err
				}
				value = strings.ReplaceAll(strings.ReplaceAll(value, "\r\n", "\n"), "\r", "\n")
			}
			script = &value
		}
		token := ""
		if o.github && !o.print {
			token, err = bootstrapGitHubToken(cmd.Context())
			if err != nil {
				return err
			}
		}
		steps, err := bootstrapSteps(o, env, agents, token, script)
		if err != nil {
			return err
		}
		if len(steps) == 0 {
			return &ExitError{2, "Error: Nothing to do. Pass at least one of --setup-github, --install, --install-avr, --repo, --dotfiles, --env, or --run."}
		}
		if o.print {
			for index, step := range steps {
				if _, err = fmt.Fprintf(d.Out, "# [%d/%d] %s\n", index+1, len(steps), output.CellText(step.description)); err != nil {
					return err
				}
				if step.secret {
					if _, err = fmt.Fprintln(d.Out, "#   (a secret is fed to this step over stdin and is not shown)"); err != nil {
						return err
					}
				}
				if _, err = fmt.Fprintln(d.Out, step.script+"\n"); err != nil {
					return err
				}
			}
			return nil
		}
		cfg, api, orgID, err := organizationContext(cmd.Context(), d, o.org)
		if err != nil {
			return err
		}
		ep, err := vmSSHEndpoint(cmd.Context(), cfg, api, orgID, args[0])
		if err != nil {
			return err
		}
		return runBootstrapSteps(cmd.Context(), d, ep, steps, o.identity)
	})
	return c
}
