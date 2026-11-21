// Package plugins holds the plugins this repository publishes for Claude Code
// (.claude-plugin/marketplace.json), Codex (.agents/plugins/marketplace.json)
// and Cursor (.cursor-plugin/marketplace.json). Each plugins/<name> folder is what
// a plugin install or a directory submission ships, so Go code stays here,
// outside it.
package plugins

import _ "embed"

// AvreaSkill is the avrea-cli skill. avr skill install writes the same file,
// so the plugin, skills.sh and avr install one skill.
//
//go:embed avrea-cli/skills/avrea-cli/SKILL.md
var AvreaSkill string
