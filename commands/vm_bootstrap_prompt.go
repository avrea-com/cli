package commands

import (
	"bufio"
	"context"
	"fmt"
	"strings"
)

func bootstrapClaudePrompt(ctx context.Context, d Dependencies, env *bootstrapEnvironment) error {
	if !d.IsInputTerminal() || !d.IsTerminal() {
		return nil
	}
	if _, err := fmt.Fprintln(d.Err, "Claude has no credential to use on the VM (no ANTHROPIC_API_KEY or CLAUDE_CODE_OAUTH_TOKEN set locally)."); err != nil {
		return err
	}
	reader := bufio.NewReader(d.In)
	setup := false
	for {
		answer, err := cardPrompt(ctx, d, reader, "Run `claude setup-token` now to create a 1-year subscription token? [y/N]", false)
		if err != nil {
			return err
		}
		switch strings.ToLower(strings.TrimSpace(answer)) {
		case "y", "yes":
			setup = true
		case "", "n", "no":
		default:
			if _, err = fmt.Fprintln(d.Out, "Error: invalid input"); err != nil {
				return err
			}
			continue
		}
		break
	}
	if setup {
		childCtx, cancel := context.WithCancel(ctx)
		process, err := vmStartProcess(childCtx, d, []string{"claude", "setup-token"})
		if err != nil {
			cancel()
			if ctx.Err() != nil {
				return ctx.Err()
			}
			if _, err = fmt.Fprintln(d.Err, "Could not launch `claude` (is it installed?). Paste an existing token below, or skip."); err != nil {
				return err
			}
		} else {
			err = process.wait(ctx)
			cancel()
			<-process.done
			if err != nil {
				return err
			}
		}
	}
	token, err := cardPrompt(ctx, d, reader, "Paste the token (sk-ant-...), or leave blank to skip", true)
	if err != nil {
		return err
	}
	token = strings.TrimSpace(token)
	if token == "" {
		_, err = fmt.Fprintln(d.Err, "No token provided; claude will be installed but left unauthenticated on the VM.")
		return err
	}
	if !strings.HasPrefix(token, "sk-ant-") {
		if _, err = fmt.Fprintln(d.Err, "Warning: that does not look like a Claude token (expected sk-ant-...); forwarding it anyway."); err != nil {
			return err
		}
	}
	env.set("CLAUDE_CODE_OAUTH_TOKEN", token)
	_, err = fmt.Fprintln(d.Err, "Forwarding the token as CLAUDE_CODE_OAUTH_TOKEN. Tip: `export CLAUDE_CODE_OAUTH_TOKEN=...` locally to reuse it on the next re-bootstrap instead of minting a new one each boot.")
	return err
}
