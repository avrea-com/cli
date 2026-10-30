package commands

import (
	"fmt"
	"runtime"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/avrea-com/cli/output"
)

func vmEndpoints(value any) string {
	endpoints, _ := value.(map[string]any)
	if len(endpoints) == 0 {
		return "(pending: VM not RUNNING yet)"
	}
	parts := []string{}
	if ssh, ok := endpoints["ssh"].(map[string]any); ok && len(ssh) > 0 {
		parts = append(parts, "ssh "+pythonValue(ssh["username"])+"@"+pythonValue(ssh["external_ip"])+" -p "+pythonValue(ssh["external_port"]))
	}
	if rd, ok := endpoints["remote_desktop"].(map[string]any); ok && len(rd) > 0 {
		parts = append(parts, pythonValue(rd["protocol"])+" "+pythonValue(rd["external_ip"])+":"+pythonValue(rd["external_port"]))
	}
	if len(parts) == 0 {
		return "(none)"
	}
	return strings.Join(parts, "; ")
}
func vmPasswordQuote(password string) string {
	alnum := password != ""
	for _, r := range password {
		if !unicode.IsLetter(r) && !unicode.IsNumber(r) {
			alnum = false
			break
		}
	}
	if alnum {
		return password
	}
	return "'" + strings.ReplaceAll(password, "'", "'\\''") + "'"
}
func vmFreeRDP(binary, address, user, password string, rfx bool, cert string) string {
	parts := []string{binary + " /v:" + address + " /u:" + user}
	if password != "" {
		parts = append(parts, "/p:"+vmPasswordQuote(password))
	}
	if rfx {
		parts = append(parts, "/gfx:rfx")
	}
	parts = append(parts, "+clipboard")
	if cert != "" {
		parts = append(parts, "/cert:"+cert)
	} else if password != "" {
		parts = append(parts, "/cert:tofu")
	}
	return strings.Join(parts, " ")
}
func vmRDPConnect(address, user, password, platform string, rfx bool, cert string) []string {
	switch platform {
	case "darwin":
		lines := []string{"open \"rdp://full%20address=s:" + address + "&username=s:" + user + "\""}
		if password != "" {
			lines = append(lines, "(no password in the URI; use the one-time password shown below)")
		}
		lines = append(lines, vmFreeRDP("sdl3-freerdp", address, user, password, rfx, cert), "(brew install freerdp; binary is sdl-freerdp on older installs)")
		return lines
	case "windows":
		if password == "" {
			return []string{"mstsc /v:" + address}
		}
		host, _, _ := strings.Cut(address, ":")
		return []string{"cmdkey /generic:TERMSRV/" + host + " /user:" + user + " /pass:" + password, "mstsc /v:" + address, "cmdkey /delete:TERMSRV/" + host, "(run the delete afterwards; the credential persists until deleted)"}
	default:
		return []string{vmFreeRDP("xfreerdp", address, user, password, rfx, cert)}
	}
}
func vmConnect(vm map[string]any, password string) []string {
	if vm["enable_remote_desktop"] != true {
		return nil
	}
	eps, _ := vm["endpoints"].(map[string]any)
	rd, _ := eps["remote_desktop"].(map[string]any)
	pending := len(rd) == 0
	address, user := "IP:PORT", "USER"
	if pending {
		if vm["os_type"] == "macos" {
			return nil
		}
	} else {
		if rd["protocol"] != "rdp" {
			return nil
		}
		address = pythonValue(rd["external_ip"]) + ":" + pythonValue(rd["external_port"])
		user = pythonValue(fallback(rd["username"], "USER"))
	}
	lines := vmRDPConnect(address, user, password, runtime.GOOS, vm["os_type"] == "linux", "")
	if pending {
		lines = append(lines, "(IP:PORT appears in `avr vm show "+pythonValue(vm["customer_vm_id"])+"` once the VM is RUNNING)")
	}
	return append(lines, "(first connect shows a self-signed certificate warning; accept to continue)")
}
func renderVM(d Dependencies, vm map[string]any, password string) error {
	value := func(key string) string { return pythonValue(vm[key]) }
	desktop := "no"
	if vm["enable_remote_desktop"] == true {
		desktop = "yes"
	}
	pairs := [][2]string{
		{"VM ID", value("customer_vm_id")}, {"Name", value("display_name")}, {"OS", value("os_type")},
		{"Resources", value("cpu_count") + " vCPU / " + value("memory_mb") + " MB / " + value("disk_gb") + " GB disk"},
		{"State", value("state")}, {"Desired state", value("desired_state")}, {"Reason", pythonValue(fallback(vm["state_reason"], "-"))},
		{"Remote desktop", desktop}, {"Endpoints", vmEndpoints(vm["endpoints"])},
	}
	if ref := fieldText(vm, "precheckout_ref"); ref != "" {
		pairs = append(pairs, [2]string{"Preload", ref + " (" + pythonValue(fallback(vm["preload_status"], "-")) + ")"})
	}
	if depth := vm["fetch_depth"]; depth != nil {
		text := pythonValue(depth)
		if text == "0" {
			text = "0 (all branches and tags)"
		}
		pairs = append(pairs, [2]string{"Fetch depth", text})
	}
	if snapshot, ok := vm["snapshot"].(map[string]any); ok && len(snapshot) > 0 {
		kind := "disk only"
		if snapshot["memory_included"] == true {
			kind = "memory + disk"
		}
		pairs = append(pairs, [2]string{"Snapshot", kind})
		if at := fieldText(snapshot, "paused_at"); at != "" {
			pairs = append(pairs, [2]string{"Paused at", at})
		}
	}
	pairs = append(pairs, [2]string{"Auto-stop at", value("stop_at")}, [2]string{"Created", value("created_at")})
	width := 0
	for _, pair := range pairs {
		width = max(width, utf8.RuneCountInString(pair[0]))
	}
	for _, pair := range pairs {
		if _, err := fmt.Fprintf(d.Out, "%-*s %s\n", width+1, pair[0], output.CellText(pair[1])); err != nil {
			return err
		}
		if pair[0] == "Remote desktop" {
			for index, line := range vmConnect(vm, password) {
				label := ""
				if index == 0 {
					label = "Connect"
				}
				if _, err := fmt.Fprintf(d.Out, "%-*s %s\n", width+1, label, output.CellText(line)); err != nil {
					return err
				}
			}
		}
	}
	if keys, ok := vm["ssh_public_keys"].([]any); ok && len(keys) > 0 {
		_, err := fmt.Fprintf(d.Out, "SSH keys     %d configured\n", len(keys))
		return err
	}
	return nil
}
