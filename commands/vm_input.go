package commands

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"os"
	"strings"
	"unicode/utf8"
)

func parseVMTTL(value string) (int, error) {
	text := strings.ToLower(strings.TrimSpace(value))
	multiplier := int64(1)
	if len(text) > 0 {
		if scale, ok := map[byte]int64{'s': 1, 'm': 60, 'h': 3600, 'd': 86400}[text[len(text)-1]]; ok {
			multiplier = scale
			text = strings.TrimSpace(text[:len(text)-1])
		}
	}
	if !settingInteger().MatchString(text) {
		return 0, &ExitError{2, "Error: Invalid value for --ttl: invalid duration " + pythonQuote(value) + ": use e.g. 8h, 7d, 1800s, or a number of seconds"}
	}
	number, ok := new(big.Int).SetString(strings.ReplaceAll(text, "_", ""), 10)
	if !ok {
		return 0, &ExitError{2, "Error: Invalid value for --ttl: invalid duration"}
	}
	number.Mul(number, big.NewInt(multiplier))
	if number.Cmp(big.NewInt(300)) < 0 || number.Cmp(big.NewInt(604800)) > 0 {
		return 0, &ExitError{2, fmt.Sprintf("Error: Invalid value for --ttl: TTL must be between 300s and 604800s (got %ss)", number.String())}
	}
	return int(number.Int64()), nil
}
func vmInputFile(raw, flag, kind string) (string, string, error) {
	path, err := expandArtifactPath(raw)
	fail := func(err error) (string, string, error) {
		return "", path, &ExitError{2, fmt.Sprintf("Error: Invalid value for %s: cannot read %s file %s: %s", flag, kind, path, err)}
	}
	if err != nil {
		return fail(err)
	}
	file, err := os.Open(path)
	if err != nil {
		return fail(err)
	}
	defer func() { _ = file.Close() }()
	data, err := io.ReadAll(io.LimitReader(file, (16<<20)+1))
	if err != nil {
		return fail(err)
	}
	if len(data) > 16<<20 {
		return fail(errors.New("file exceeds 16 MiB"))
	}
	if !utf8.Valid(data) {
		return fail(errors.New("file is not valid UTF-8"))
	}
	return string(data), path, nil
}
func resolveVMSSHKeys(values []string) ([]string, error) {
	keys := []string{}
	for _, value := range values {
		content, source := strings.TrimSpace(value), "the --ssh-key value"
		if strings.HasPrefix(value, "@") {
			data, path, err := vmInputFile(value[1:], "--ssh-key", "SSH key")
			if err != nil {
				return nil, err
			}
			content, source = strings.TrimSpace(data), path
			if content == "" {
				return nil, &ExitError{2, "Error: Invalid value for --ssh-key: SSH key file " + path + " is empty"}
			}
		}
		if strings.Contains(content, "PRIVATE KEY") {
			return nil, &ExitError{2, "Error: Invalid value for --ssh-key: " + source + " looks like a private key, not a public key. Point --ssh-key at the .pub file."}
		}
		parts := strings.Fields(content)
		valid := false
		if len(parts) > 0 {
			for _, prefix := range []string{"ssh-ed25519", "ssh-rsa", "ssh-dss", "ecdsa-sha2-", "sk-ssh-ed25519@", "sk-ecdsa-sha2-"} {
				if strings.HasPrefix(parts[0], prefix) {
					valid = true
					break
				}
			}
		}
		if !valid {
			return nil, &ExitError{2, "Error: Invalid value for --ssh-key: " + source + " is not a recognized SSH public key (expected a line starting with e.g. ssh-ed25519 or ssh-rsa)."}
		}
		keys = append(keys, content)
	}
	return keys, nil
}
func parseVMEgress(value string) ([]any, error) {
	raw := value
	if strings.HasPrefix(value, "@") {
		data, _, err := vmInputFile(value[1:], "--egress-rules", "egress rules")
		if err != nil {
			return nil, err
		}
		raw = data
	}
	decoder := json.NewDecoder(strings.NewReader(raw))
	decoder.UseNumber()
	var decoded any
	if err := decoder.Decode(&decoded); err != nil {
		return nil, &ExitError{2, "Error: Invalid value for --egress-rules: --egress-rules must be valid JSON"}
	}
	var extra any
	if decoder.Decode(&extra) != io.EOF {
		return nil, &ExitError{2, "Error: Invalid value for --egress-rules: --egress-rules must be valid JSON"}
	}
	rules, ok := decoded.([]any)
	if !ok {
		return nil, &ExitError{2, "Error: Invalid value for --egress-rules: --egress-rules must be a JSON array of rule objects"}
	}
	return rules, nil
}
