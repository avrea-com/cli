package commands

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"unicode/utf8"

	"github.com/spf13/cobra"
)

const configurationValueLimit = 48 * 1024

var configurationName = lazyRegexp(`^[A-Za-z_][A-Za-z0-9_]*$`)

type configurationInput struct{ name, value string }

func validateConfigurationName(name string) error {
	if len(name) > 256 || !configurationName().MatchString(name) {
		return configurationUsage("Names must be 1–256 ASCII letters, digits, or underscores, and cannot start with a digit.")
	}
	for _, prefix := range []string{"GITHUB_", "GITLAB_", "CI_", "AVREA_"} {
		if strings.HasPrefix(strings.ToUpper(name), prefix) {
			return configurationUsage("Names cannot start with GITHUB_, GITLAB_, CI_, or AVREA_.")
		}
	}
	return nil
}
func validateConfigurationDescription(description string) error {
	if utf8.RuneCountInString(description) > 1024 || strings.ContainsRune(description, 0) {
		return configurationUsage("Descriptions must be at most 1024 characters without NUL bytes.")
	}
	return nil
}
func validateConfigurationValue(value string) error {
	if len(value) > configurationValueLimit {
		return configurationUsage("Values must not exceed 48 KiB.")
	}
	if !utf8.ValidString(value) || strings.ContainsRune(value, 0) {
		return configurationUsage("Values must be UTF-8 text without NUL bytes.")
	}
	return nil
}
func configurationInputValue(c *cobra.Command, d Dependencies, kind, body string, asFile bool) (string, error) {
	if c.Flags().Changed("body") {
		return body, validateConfigurationValue(body)
	}
	if d.IsInputTerminal() {
		if asFile {
			return "", configurationUsage("File secrets require piped stdin; use avr secret set NAME --type file < private-key.pem.")
		}
		if d.Getenv("AVR_PROMPT_DISABLED") != "" {
			return "", configurationUsage("Value input is required; pipe the value on stdin.")
		}
		// A hidden prompt requires a real terminal; never silently echo a secret.
		if kind == "secret" {
			if _, ok := d.In.(*os.File); !ok {
				return "", configurationUsage("Secret input requires a terminal or piped stdin.")
			}
		}
		prompt := configurationPrompt(d)
		value, err := cardPrompt(c.Context(), prompt, bufio.NewReader(d.In), "Value", kind == "secret")
		if err != nil {
			return "", err
		}
		return value, validateConfigurationValue(value)
	}
	type inputResult struct {
		value string
		err   error
	}
	done := make(chan inputResult, 1)
	go func() {
		value, err := readConfigurationInput(c.Context(), d.In, asFile)
		done <- inputResult{value, err}
	}()
	select {
	case <-c.Context().Done():
		return "", c.Context().Err()
	case result := <-done:
		if err := c.Context().Err(); err != nil {
			return "", err
		}
		return result.value, result.err
	}
}

func readConfigurationInput(ctx context.Context, input io.Reader, asFile bool) (string, error) {
	if asFile {
		raw, err := io.ReadAll(io.LimitReader(input, configurationValueLimit+1))
		if err != nil {
			return "", fmt.Errorf("could not read value input")
		}
		value := string(raw)
		return value, validateConfigurationValue(value)
	}
	// Match gh's stdin trimming; retain a bounded pending newline suffix so a
	// later non-newline cannot turn an oversized input into an accepted prefix.
	// https://github.com/cli/cli/blob/e1219499a578b69e3437adaa31cd60614e23c802/pkg/cmd/secret/set/set.go#L413-L432
	var value, pending []byte
	buffer := make([]byte, 4096)
	for {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		n, err := input.Read(buffer)
		for _, ch := range buffer[:n] {
			if ch == '\r' || ch == '\n' {
				if len(pending) <= configurationValueLimit {
					pending = append(pending, ch)
				}
				continue
			}
			if len(value)+len(pending)+1 > configurationValueLimit {
				return "", configurationUsage("Values must not exceed 48 KiB.")
			}
			value = append(value, pending...)
			pending = pending[:0]
			value = append(value, ch)
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			return "", fmt.Errorf("could not read value input")
		}
	}
	result := string(value)
	return result, validateConfigurationValue(result)
}

// Parse literal dotenv entries: quotes and multiline values are supported, but
// shell expansion and variable interpolation are intentionally never evaluated.
func configurationEnvFile(path string) ([]configurationInput, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("could not open --env-file")
	}
	defer func() { _ = f.Close() }()
	const fileLimit = 16 * 1024 * 1024
	raw, err := io.ReadAll(io.LimitReader(f, fileLimit+1))
	if err != nil {
		return nil, fmt.Errorf("could not read --env-file")
	}
	if len(raw) > fileLimit || !utf8.Valid(raw) || strings.ContainsRune(string(raw), 0) {
		return nil, configurationUsage("The dotenv file must be UTF-8 text without NUL bytes, at most 16 MiB.")
	}
	text := strings.ReplaceAll(string(raw), "\r\n", "\n")
	inputs := []configurationInput{}
	seen := map[string]bool{}
	line := 0
	for text != "" {
		line++
		current, rest, _ := strings.Cut(text, "\n")
		text = rest
		current = strings.TrimLeft(current, " \t")
		if strings.TrimSpace(current) == "" || strings.HasPrefix(current, "#") {
			continue
		}
		if strings.HasPrefix(current, "export ") {
			current = strings.TrimLeft(strings.TrimPrefix(current, "export "), " \t")
		}
		key, value, ok := strings.Cut(current, "=")
		key = strings.TrimSpace(key)
		bad := func() error {
			return configurationUsage(fmt.Sprintf("Invalid dotenv entry at line %d; expected NAME=value with balanced quotes.", line))
		}
		if !ok {
			return nil, bad()
		}
		if err := validateConfigurationName(key); err != nil {
			return nil, configurationUsage(fmt.Sprintf("Invalid dotenv name at line %d.", line))
		}
		key = strings.ToUpper(key)
		if seen[key] {
			return nil, configurationUsage(fmt.Sprintf("Duplicate dotenv name %s.", key))
		}
		seen[key] = true
		value = strings.TrimLeft(value, " \t")
		if strings.HasPrefix(value, "\"") || strings.HasPrefix(value, "'") {
			quote := value[0]
			source := value[1:]
			var result strings.Builder
			closed := false
			for !closed {
				for i := 0; i < len(source); i++ {
					ch := source[i]
					if ch == quote {
						suffix := strings.TrimSpace(source[i+1:])
						if suffix != "" && !strings.HasPrefix(suffix, "#") {
							return nil, bad()
						}
						closed = true
						break
					}
					if ch == '\\' && quote == '"' && i+1 < len(source) {
						i++
						switch source[i] {
						case 'n':
							result.WriteByte('\n')
						case 'r':
							result.WriteByte('\r')
						case 't':
							result.WriteByte('\t')
						case '\\', '"':
							result.WriteByte(source[i])
						default:
							result.WriteByte('\\')
							result.WriteByte(source[i])
						}
						continue
					}
					result.WriteByte(ch)
				}
				if !closed {
					if text == "" {
						return nil, bad()
					}
					result.WriteByte('\n')
					source, rest, _ = strings.Cut(text, "\n")
					text = rest
					line++
				}
			}
			value = result.String()
		} else {
			value = strings.TrimSpace(value)
			for i, ch := range value {
				if ch == '#' && (i == 0 || value[i-1] == ' ' || value[i-1] == '\t') {
					value = strings.TrimSpace(value[:i])
					break
				}
			}
		}
		if err := validateConfigurationValue(value); err != nil {
			return nil, configurationUsage(fmt.Sprintf("Invalid value for %s: values must be UTF-8 text of at most 48 KiB without NUL bytes.", key))
		}
		inputs = append(inputs, configurationInput{key, value})
	}
	if len(inputs) == 0 {
		return nil, configurationUsage("The dotenv file has no entries.")
	}
	return inputs, nil
}
