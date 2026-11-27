package commands

import (
	"encoding/xml"
	"strings"

	"github.com/kballard/go-shellquote"
)

// parseOSRelease reads the shell-compatible assignments of os-release(5):
// https://www.freedesktop.org/software/systemd/man/latest/os-release.html
func parseOSRelease(data []byte) string {
	values := make(map[string]string)
	for line := range strings.SplitSeq(string(data), "\n") {
		key, raw, ok := strings.Cut(strings.TrimSpace(line), "=")
		if !ok || (key != "PRETTY_NAME" && key != "NAME" && key != "VERSION_ID") {
			continue
		}
		words, err := shellquote.Split(osReleaseWithoutComment(raw))
		if err != nil || len(words) > 1 {
			continue
		}
		values[key] = ""
		if len(words) == 1 {
			values[key] = words[0]
		}
	}
	if pretty := values["PRETTY_NAME"]; pretty != "" {
		return pretty
	}
	if name := values["NAME"]; name != "" {
		return strings.TrimSpace(name + " " + values["VERSION_ID"])
	}
	return ""
}

func osReleaseWithoutComment(raw string) string {
	var quote rune
	escaped := false
	for i, c := range raw {
		switch {
		case escaped:
			escaped = false
		case c == '\\' && quote != '\'':
			escaped = true
		case c == quote:
			quote = 0
		case quote != 0:
		case c == '\'' || c == '"':
			quote = c
		case c == '#' && (i == 0 || raw[i-1] == ' ' || raw[i-1] == '\t'):
			return raw[:i]
		}
	}
	return raw
}

func parseSystemVersionPlist(data []byte) string {
	var plist struct {
		XMLName xml.Name `xml:"plist"`
		Dict    struct {
			Items []struct {
				XMLName xml.Name
				Value   string `xml:",chardata"`
			} `xml:",any"`
		} `xml:"dict"`
	}
	if err := xml.Unmarshal(data, &plist); err != nil {
		return ""
	}
	for i, item := range plist.Dict.Items {
		if item.XMLName.Local == "key" && item.Value == "ProductVersion" && i+1 < len(plist.Dict.Items) {
			value := plist.Dict.Items[i+1]
			if value.XMLName.Local == "string" && value.Value != "" {
				return "macOS " + value.Value
			}
		}
	}
	return ""
}
