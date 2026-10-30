package commands

import (
	"context"
	"fmt"
	"math/big"
	"net"
	"net/url"
	"runtime"
	"strconv"
	"strings"

	"github.com/avrea-com/cli/output"
	"github.com/spf13/cobra"
)

type vmForward struct{ local, guest int }

func vmPort(value, label string) (int, error) {
	text := strings.TrimSpace(value)
	if !settingInteger.MatchString(text) {
		return 0, &ExitError{2, "Error: --port " + label + " '" + output.CellText(value) + "' is not an integer."}
	}
	number, ok := new(big.Int).SetString(strings.ReplaceAll(text, "_", ""), 10)
	if !ok {
		return 0, &ExitError{2, "Error: --port " + label + " '" + output.CellText(value) + "' is not an integer."}
	}
	if number.Cmp(big.NewInt(1)) < 0 || number.Cmp(big.NewInt(65535)) > 0 {
		return 0, &ExitError{2, "Error: --port " + label + " " + number.String() + " is out of range (1-65535)."}
	}
	return int(number.Int64()), nil
}
func vmPickPort(ctx context.Context, d Dependencies) (int, error) {
	listener, err := d.Listen(ctx, "tcp4", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	defer func() { _ = listener.Close() }()
	address, ok := listener.Addr().(*net.TCPAddr)
	if !ok {
		return 0, fmt.Errorf("unexpected local listener address")
	}
	return address.Port, nil
}
func vmForwards(ctx context.Context, d Dependencies, specs []string, override int) ([]vmForward, error) {
	if override != 0 && (len(specs) != 1 || strings.Contains(specs[0], ":")) {
		return nil, &ExitError{2, "Error: --local-port applies to a single bare --port; for multiple ports use --port LOCAL:GUEST."}
	}
	forwards := []vmForward{}
	locals, guests := map[int]bool{}, map[int]bool{}
	for _, spec := range specs {
		localText, guestText := "", spec
		split := strings.LastIndex(spec, ":")
		if split >= 0 {
			localText, guestText = spec[:split], spec[split+1:]
		}
		guest, err := vmPort(guestText, "guest port")
		if err != nil {
			return nil, err
		}
		if guests[guest] {
			return nil, &ExitError{2, fmt.Sprintf("Error: --port guest %d is forwarded more than once.", guest)}
		}
		guests[guest] = true
		local := override
		if split >= 0 {
			local, err = vmPort(localText, "local port")
			if err != nil {
				return nil, err
			}
		}
		if local != 0 {
			if locals[local] {
				return nil, &ExitError{2, fmt.Sprintf("Error: --port local %d is bound more than once.", local)}
			}
			locals[local] = true
		}
		forwards = append(forwards, vmForward{local, guest})
	}
	for i := range forwards {
		if forwards[i].local != 0 {
			continue
		}
		for attempts := 0; ; attempts++ {
			if attempts == 100 {
				return nil, fmt.Errorf("could not choose distinct local forwarding ports")
			}
			local, err := vmPickPort(ctx, d)
			if err != nil {
				return nil, err
			}
			if !locals[local] {
				forwards[i].local = local
				locals[local] = true
				break
			}
		}
	}
	return forwards, nil
}
func vmTunnelArgv(host, user string, port int, forwards []vmForward, identity, known string) []string {
	args := []string{"ssh", "-N", "-o", "ExitOnForwardFailure=yes", "-o", "ConnectTimeout=15", "-o", "ServerAliveInterval=30"}
	for _, forward := range forwards {
		args = append(args, "-L", fmt.Sprintf("127.0.0.1:%d:localhost:%d", forward.local, forward.guest))
	}
	args = append(args, "-p", strconv.Itoa(port))
	if known != "" {
		args = append(args, "-o", "UserKnownHostsFile="+known, "-o", "StrictHostKeyChecking=yes")
	} else {
		args = append(args, "-o", "StrictHostKeyChecking=accept-new")
	}
	if identity != "" {
		args = append(args, "-i", identity)
	}
	return append(args, user+"@"+host)
}
func vmLocalPortFlag(c *cobra.Command, value int) error {
	if c.Flags().Changed("local-port") && (value < 1 || value > 65535) {
		return &ExitError{2, "Error: Invalid value for '--local-port': must be between 1 and 65535."}
	}
	return nil
}
func vmPrintTunnelNotice(d Dependencies) error {
	_, err := fmt.Fprintln(d.Err, "(the live command pins the VM's SSH host key; this printed form uses trust-on-first-use)")
	return err
}
func newVMPortForward(d Dependencies) *cobra.Command {
	var org, identity string
	var specs []string
	var local int
	var printOnly bool
	c := &cobra.Command{Use: "port-forward VM_ID", Short: "Forward local TCP ports to a VM over SSH.", Args: positionalArguments("VM_ID")}
	orgFlag(c, &org)
	f := c.Flags()
	f.StringArrayVar(&specs, "port", nil, "Guest port or LOCAL:GUEST; repeatable.")
	_ = c.MarkFlagRequired("port")
	c.Args = func(cmd *cobra.Command, args []string) error {
		if err := positionalArguments("VM_ID")(cmd, args); err != nil {
			return err
		}
		if len(specs) == 0 {
			return &ExitError{2, "Error: Missing option '--port'."}
		}
		return nil
	}
	f.IntVar(&local, "local-port", 0, "Local port for one bare --port; otherwise choose an unused port.")
	f.StringVarP(&identity, "identity", "i", "", "SSH private key file.")
	f.BoolVar(&printOnly, "print", false, "Print the SSH tunnel command without connecting.")
	c.RunE = run(func(cmd *cobra.Command, args []string) error {
		if err := vmLocalPortFlag(cmd, local); err != nil {
			return err
		}
		forwards, err := vmForwards(cmd.Context(), d, specs, local)
		if err != nil {
			return err
		}
		cfg, api, orgID, err := organizationContext(cmd.Context(), d, org)
		if err != nil {
			return err
		}
		ep, err := vmSSHEndpoint(cmd.Context(), cfg, api, orgID, args[0])
		if err != nil {
			return err
		}
		host, user, port, err := vmSSHAddress(ep)
		if err != nil {
			return err
		}
		if printOnly {
			if _, err = fmt.Fprintln(d.Out, vmShellJoin(vmTunnelArgv(host, user, port, forwards, identity, ""))); err != nil {
				return err
			}
			return vmPrintTunnelNotice(d)
		}
		routes, binds := []string{}, []string{}
		for _, forward := range forwards {
			address := fmt.Sprintf("127.0.0.1:%d", forward.local)
			routes = append(routes, fmt.Sprintf("%s -> %s:%d", address, output.CellText(args[0]), forward.guest))
			binds = append(binds, address)
		}
		if _, err = fmt.Fprintln(d.Err, "Forwarding "+strings.Join(routes, ", ")+" ..."); err != nil {
			return err
		}
		return vmRunTunnel(cmd.Context(), d, ep, forwards, identity, func(proc *vmTunnelProcess) error {
			if _, err := fmt.Fprintln(d.Err, "Tunnel is up on "+strings.Join(binds, ", ")+". Press Ctrl-C to close it."); err != nil {
				return err
			}
			return proc.wait(cmd.Context())
		})
	})
	return c
}
func vmDesktopLines(protocol, address, user, guestOS, platform string) []string {
	if protocol == "vnc" {
		if platform == "darwin" {
			return []string{"open vnc://" + address, "(opens macOS Screen Sharing)"}
		}
		return []string{"point a VNC client at " + address}
	}
	return vmRDPConnect(address, user, "", platform, guestOS == "linux", "ignore")
}
func vmDesktopLaunch(protocol, address, user, guestOS, platform string) ([]string, bool) {
	if protocol == "vnc" {
		if platform == "darwin" {
			return []string{"open", "vnc://" + address}, true
		}
		return nil, false
	}
	switch platform {
	case "windows":
		return []string{"mstsc", "/v:" + address}, false
	case "darwin":
		return []string{"open", "rdp://full%20address=s:" + address + "&username=s:" + user}, true
	default:
		args := []string{"xfreerdp", "/v:" + address, "/u:" + user, "+clipboard", "/cert:ignore"}
		if guestOS == "linux" {
			args = append(args, "/gfx:rfx")
		}
		return args, false
	}
}
func newVMDesktop(d Dependencies, protocol string) *cobra.Command {
	var org, identity string
	var local int
	var launch, printOnly bool
	c := &cobra.Command{Use: protocol + " VM_ID", Short: "Open a " + strings.ToUpper(protocol) + " desktop over an SSH tunnel.", Args: positionalArguments("VM_ID")}
	orgFlag(c, &org)
	f := c.Flags()
	f.IntVar(&local, "local-port", 0, "Local port to bind; defaults to an unused port.")
	f.StringVarP(&identity, "identity", "i", "", "SSH private key file.")
	f.BoolVar(&launch, "launch", false, "Launch a local desktop client.")
	f.Var(inverseBool{&launch}, "no-launch", "Print connection instructions without launching a client.")
	f.Lookup("no-launch").NoOptDefVal = "true"
	f.BoolVar(&printOnly, "print", false, "Print tunnel and client commands without connecting.")
	c.RunE = run(func(cmd *cobra.Command, args []string) error {
		if err := vmLocalPortFlag(cmd, local); err != nil {
			return err
		}
		cfg, api, orgID, err := organizationContext(cmd.Context(), d, org)
		if err != nil {
			return err
		}
		record, err := api.VM(cmd.Context(), vmBase(orgID)+"/"+url.PathEscape(args[0]))
		if err != nil {
			return vmError(cfg, err, "fetch VM")
		}
		row := record.Fields
		if row["enable_remote_desktop"] != true {
			return &ExitError{1, "Error: VM " + output.CellText(args[0]) + " has no remote desktop. Recreate it with `--remote-desktop`."}
		}
		eps, _ := row["endpoints"].(map[string]any)
		ep, _ := eps["ssh"].(map[string]any)
		if len(ep) == 0 {
			return vmMissingSSHEndpoint(row, args[0])
		}
		rd, _ := eps["remote_desktop"].(map[string]any)
		guestOS := fieldText(row, "os_type")
		actual := "rdp"
		if guestOS == "macos" {
			actual = "vnc"
		}
		if len(rd) > 0 {
			actual = billingText(rd, "protocol", "None")
		}
		if actual != protocol {
			alternative := "rdp"
			if actual == "vnc" {
				alternative = "vnc"
			}
			return &ExitError{1, fmt.Sprintf("Error: VM %s speaks %s, not %s. Use `avr vm %s %s` instead.", output.CellText(args[0]), output.CellText(actual), protocol, alternative, output.CellText(args[0]))}
		}
		host, sshUser, port, err := vmSSHAddress(ep)
		if err != nil {
			return err
		}
		user := fieldText(rd, "username")
		if user == "" {
			user = sshUser
		}
		if local == 0 {
			local, err = vmPickPort(cmd.Context(), d)
			if err != nil {
				return err
			}
		}
		guestPort := 3389
		if protocol == "vnc" {
			guestPort = 5900
		}
		forwards := []vmForward{{local, guestPort}}
		address := fmt.Sprintf("127.0.0.1:%d", local)
		lines := vmDesktopLines(protocol, address, user, guestOS, runtime.GOOS)
		if printOnly {
			if _, err = fmt.Fprintln(d.Out, "Open the tunnel:\n  "+vmShellJoin(vmTunnelArgv(host, sshUser, port, forwards, identity, ""))+"\nThen connect with:"); err != nil {
				return err
			}
			for _, line := range lines {
				if _, err = fmt.Fprintln(d.Out, "  "+output.CellText(line)); err != nil {
					return err
				}
			}
			return vmPrintTunnelNotice(d)
		}
		if _, err = fmt.Fprintf(d.Err, "Opening SSH tunnel %s -> %s:%d ...\n", address, output.CellText(args[0]), guestPort); err != nil {
			return err
		}
		return vmRunTunnel(cmd.Context(), d, ep, forwards, identity, func(proc *vmTunnelProcess) error {
			if _, err := fmt.Fprintln(d.Err, "Connect with:"); err != nil {
				return err
			}
			for _, line := range lines {
				if _, err := fmt.Fprintln(d.Err, "  "+output.CellText(line)); err != nil {
					return err
				}
			}
			if _, err := fmt.Fprintln(d.Err, "(use the one-time password saved at create / last start)"); err != nil {
				return err
			}
			if launch {
				argv, detaches := vmDesktopLaunch(protocol, address, user, guestOS, runtime.GOOS)
				return vmLaunchDesktop(cmd.Context(), d, argv, detaches, proc)
			}
			if _, err := fmt.Fprintln(d.Err, "Tunnel is up. Press Ctrl-C to close it."); err != nil {
				return err
			}
			return proc.wait(cmd.Context())
		})
	})
	return c
}
func vmMissingSSHEndpoint(row map[string]any, id string) error {
	return &ExitError{1, "Error: VM " + output.CellText(id) + " has no SSH endpoint yet (state: " + billingText(row, "state", "None") + "). Endpoints appear once the VM is RUNNING; check `avr vm show`."}
}
