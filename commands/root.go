// Package commands exposes fresh Cobra command constructors for avr and avr-admin.
package commands

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"runtime"
	"strings"
	"time"

	"github.com/avrea-com/cli/client"
	"github.com/avrea-com/cli/config"
	"github.com/avrea-com/cli/output"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

// Dependencies are explicit so admin can reuse commands without package globals.
type Dependencies struct {
	// UpdateNotices supports injected notices. StartUpdateCheck is opt-in for the
	// public executable; composed admin commands do not inherit a release feed.
	StartUpdateCheck func(context.Context) <-chan string

	UpdateNotices   <-chan string
	Verbose         func() bool
	ColorEnabled    func() bool
	RunSSH          func(context.Context, []string) (int, error)
	Store           *config.Store
	LoadConfig      func() (config.Config, error)
	HTTP            *http.Client
	In              io.Reader
	Out             io.Writer
	Err             io.Writer
	Getenv          func(string) string
	LookupEnv       func(string) (string, bool)
	OpenBrowser     func(context.Context, string) error
	Listen          func(context.Context, string, string) (net.Listener, error)
	Hostname        func() (string, error)
	Now             func() time.Time
	Wait            func(context.Context, time.Duration) error
	IsTerminal      func() bool
	IsInputTerminal func() bool
	IsErrorTerminal func() bool
	LoginTimeout    time.Duration
	GitRemote       func(context.Context) (string, error)
	Version         string
}

func (d Dependencies) defaults() Dependencies {
	if d.In == nil {
		d.In = os.Stdin
	}
	if d.Out == nil {
		d.Out = os.Stdout
	}
	if d.Err == nil {
		d.Err = os.Stderr
	}
	if d.Getenv == nil {
		d.Getenv = os.Getenv
		if d.LookupEnv == nil {
			d.LookupEnv = os.LookupEnv
		}
	}
	if d.LookupEnv == nil {
		d.LookupEnv = func(key string) (string, bool) { value := d.Getenv(key); return value, value != "" }
	}
	if d.GitRemote == nil {
		d.GitRemote = gitOrigin
	}
	if d.Listen == nil {
		d.Listen = (&net.ListenConfig{}).Listen
	}
	if d.Hostname == nil {
		d.Hostname = os.Hostname
	}
	if d.Wait == nil {
		d.Wait = waitPoll
	}
	if d.Now == nil {
		d.Now = time.Now
	}
	if d.IsTerminal == nil {
		d.IsTerminal = func() bool { f, ok := d.Out.(*os.File); return ok && term.IsTerminal(int(f.Fd())) }
	}
	if d.IsInputTerminal == nil {
		d.IsInputTerminal = func() bool { f, ok := d.In.(*os.File); return ok && term.IsTerminal(int(f.Fd())) }
	}
	if d.IsErrorTerminal == nil {
		d.IsErrorTerminal = func() bool { f, ok := d.Err.(*os.File); return ok && term.IsTerminal(int(f.Fd())) }
	}
	if d.LoginTimeout == 0 {
		d.LoginTimeout = 5 * time.Minute
	}
	if d.Verbose == nil {
		d.Verbose = func() bool {
			raw := strings.ToLower(strings.TrimSpace(d.Getenv("AVR_DEBUG")))
			if raw == "1" || raw == "true" || raw == "yes" || raw == "on" {
				return true
			}
			for _, category := range strings.Split(raw, ",") {
				if strings.TrimSpace(category) == "api" {
					return true
				}
			}
			return false
		}
	}
	if d.Version == "" {
		d.Version = "dev"
	}
	if d.OpenBrowser == nil {
		d.OpenBrowser = func(ctx context.Context, u string) error { return openBrowser(ctx, u, d.Getenv) }
	}
	return d
}

func (d Dependencies) configuration() (config.Config, *config.Store, error) {
	store := d.Store
	if store == nil {
		var err error
		store, err = config.DefaultStore()
		if err != nil {
			return config.Config{}, nil, err
		}
	}
	var cfg config.Config
	var err error
	if d.LoadConfig != nil {
		cfg, err = d.LoadConfig()
	} else {
		cfg, err = config.Load(store, d.Getenv)
	}
	if err == nil && cfg.StoreUnavailable {
		_, _ = fmt.Fprintln(d.Err, "Warning: could not read hosts.json; using environment credentials without stored defaults.")
	}
	return cfg, store, err
}

func (d Dependencies) load() (config.Config, *config.Store, *client.Client, error) {
	cfg, store, err := d.configuration()
	if err != nil {
		return cfg, store, nil, err
	}
	return cfg, store, d.apiClient(cfg), nil
}

func (d Dependencies) apiClient(cfg config.Config) *client.Client {
	agent := fmt.Sprintf("avr-cli/%s (%s %s; %s)", d.Version, runtime.GOOS, runtime.GOARCH, runtime.Version())
	api := client.New(cfg, d.HTTP, agent)
	if d.Verbose() {
		api.Trace = d.Err
	}
	if d.timingEnabled() {
		api.Timings = d.Err
	}
	return api
}

type ExitError struct {
	Code    int
	Message string
}

func (e *ExitError) Error() string { return e.Message }

const authHint = "To get started with Avrea CLI, please run:  avr auth login\nAlternatively, set the AVR_TOKEN environment variable to an Avrea API token."

func authenticated(cfg config.Config) error {
	if cfg.Token == "" {
		return &ExitError{4, authHint}
	}
	return nil
}

func run(fn func(*cobra.Command, []string) error) func(*cobra.Command, []string) error {
	return func(c *cobra.Command, args []string) error {
		err := fn(c, args)
		if err == nil {
			return nil
		}
		if errors.Is(err, errPagerInterrupted) {
			return err
		}
		var exit *ExitError
		if errors.As(err, &exit) {
			return err
		}
		if errors.Is(err, context.Canceled) {
			return &ExitError{130, "Interrupted."}
		}
		if isBrokenPipe(err) || errors.Is(err, io.ErrClosedPipe) {
			if closedPipeDisplay(c) {
				return &ExitError{Code: 0}
			}
			return &ExitError{1, "Error: Output closed before the command result was delivered."}
		}
		return &ExitError{1, "Error: " + err.Error()}
	}
}

// Only known display commands may finish successfully when their consumer
// stops reading. Mutations and commands used as CI gates must preserve failure.
func closedPipeDisplay(c *cobra.Command) bool {
	if gate, _ := c.Flags().GetBool("exit-status"); gate {
		return false
	}
	if web, _ := c.Flags().GetBool("web"); web {
		return false
	}
	switch canonicalHelpPath(c) {
	case "avr auth status", "avr config", "avr config list", "avr config get",
		"avr status", "avr health", "avr org list", "avr repo list",
		"avr run list", "avr run view", "avr run logs", "avr run watch", "avr run diagnose",
		"avr job list", "avr job view", "avr job logs", "avr job watch", "avr job metrics",
		"avr workflow list", "avr workflow view", "avr tests list", "avr pr list", "avr pr view",
		"avr vm list", "avr vm show", "avr vm capabilities", "avr vm metrics",
		"avr token list", "avr token view", "avr settings list", "avr cache list",
		"avr artifact list", "avr sbom list", "avr sbom view", "avr sbom diff",
		"avr sbom packages", "avr sbom vulnerabilities", "avr log search",
		"avr audit events list", "avr org install list", "avr org saml show",
		"avr org domain list", "avr billing invoice list", "avr billing invoice show":
		return true
	}
	return false
}

func NewRoot(deps Dependencies) *cobra.Command {
	d := deps.defaults()
	root := &cobra.Command{Use: "avr", Short: "Avrea CLI (Go preview)", Version: d.Version, SilenceErrors: true, SilenceUsage: true}
	root.SetIn(d.In)
	root.SetOut(d.Out)
	root.SetErr(d.Err)
	root.SetVersionTemplate("avr version {{.Version}}\n")
	var verbose bool
	inheritedVerbose := d.Verbose
	d.Verbose = func() bool { return verbose || inheritedVerbose() }
	root.PersistentFlags().BoolVarP(&verbose, "verbose", "v", false, "Show HTTP request diagnostics.")
	var noColor bool
	inheritedColor := d.ColorEnabled
	d.ColorEnabled = func() bool { return !noColor && (inheritedColor == nil || inheritedColor()) }
	root.PersistentFlags().BoolVar(&noColor, "no-color", false, "Disable colored output.")
	linksDefault := true
	linksEnv := d.Getenv("AVR_LINKS")
	switch strings.ToLower(strings.TrimSpace(linksEnv)) {
	case "":
		linksDefault = linksEnv == ""
	case "0", "false", "f", "no", "n", "off":
		linksDefault = false
	}
	root.PersistentFlags().BoolVar(&linksDefault, "links", linksDefault, "Enable links where supported.")
	root.PersistentFlags().Var(inverseBool{&linksDefault}, "no-links", "Disable terminal links.")
	root.PersistentFlags().Lookup("no-links").NoOptDefVal = "true"
	root.PersistentPreRunE = run(func(cmd *cobra.Command, _ []string) error {
		if cmd.DisableFlagParsing {
			// Custom parsers validate globals after consuming their own flags.
			return nil
		}
		return prepareGlobalOptions(cmd, d)
	})
	root.Flags().BoolP("version", "V", false, "Print version.")
	root.AddCommand(NewAuth(d), NewOrg(d), NewRepo(d), NewRun(d), NewJob(d), NewWorkflow(d), NewPR(d), NewStatus(d), NewArtifact(d), NewCache(d), NewLog(d), NewSBOM(d), NewConfig(d), NewToken(d), NewSettings(d), NewAudit(d), NewHealth(d), NewTests(d), NewBilling(d), NewFirewall(d), NewVM(d))
	login := NewLogin(d)
	login.Hidden = true
	logout := NewLogout(d)
	logout.Hidden = true
	root.AddCommand(login, logout, NewInternal(d))
	configureHelp(root, d)
	return root
}

func NewAuth(deps Dependencies) *cobra.Command {
	d := deps.defaults()
	cmd := &cobra.Command{Use: "auth", Short: "Authenticate and manage credentials."}
	cmd.SetIn(d.In)
	cmd.SetOut(d.Out)
	cmd.SetErr(d.Err)
	cmd.AddCommand(NewLogin(d), NewDeviceLogin(d), NewLogout(d), newStatus(d), newSwitch(d), newGitCredential(d))
	return cmd
}

// Execute is the standalone entry boundary; embedded consumers can execute their
// own root and inspect ExitError without a library ever calling os.Exit.
func Execute(ctx context.Context, args []string, deps Dependencies) (code int) {
	started := time.Now()
	d := deps.defaults()
	ctx, finish, err := startCPUProfile(ctx, d)
	if err != nil {
		return profileFailure(d.Err, err)
	}
	if finish != nil {
		defer finishProfile(finish, d.Err, &code)
	}
	if d.timingEnabled() && d.Getenv("_AVR_COMPLETE") == "" {
		defer func() {
			_, _ = fmt.Fprintf(d.Err, "avr-timing {\"kind\":\"command\",\"duration_ms\":%.6f}\n", float64(time.Since(started))/float64(time.Millisecond))
		}()
	}
	root := NewRoot(d) //nolint:contextcheck // Cobra passes ctx through ExecuteContext to each RunE.
	return ExecuteRoot(ctx, root, args, d.Err, d)
}

// ExecuteRoot applies the shared exit-code contract to a composed command tree.
// Optional dependencies provide environment lookup for shell completion.
func ExecuteRoot(ctx context.Context, root *cobra.Command, args []string, stderr io.Writer, dependencies ...Dependencies) (code int) {
	if code, handled := executeCompletion(root, dependencies); handled {
		return code
	}
	if stderr == nil {
		stderr = os.Stderr
	}
	d := Dependencies{Err: stderr}
	if len(dependencies) > 0 {
		d = dependencies[0]
		d.Err = stderr
	}
	d = d.defaults()
	ctx, finish, err := startCPUProfile(ctx, d)
	if err != nil {
		return profileFailure(stderr, err)
	}
	if finish != nil {
		defer finishProfile(finish, stderr, &code)
	}
	ctx = context.WithValue(ctx, organizationSlugsKey{}, &organizationSlugs{})
	root.SetArgs(expandTestsShorthand(root, args))
	previousPreRun := root.PersistentPreRunE
	root.PersistentPreRunE = func(cmd *cobra.Command, args []string) error { //nolint:contextcheck // Cobra installs ctx on the executed command.
		if previousPreRun != nil {
			if err := previousPreRun(cmd, args); err != nil {
				return err
			}
		}
		if d.StartUpdateCheck != nil && !cmd.DisableFlagParsing && updateNoticeAllowed(cmd, d) {
			d.UpdateNotices = d.StartUpdateCheck(cmd.Context())
		}
		return nil
	}
	defer func() { root.PersistentPreRunE = previousPreRun }()
	executed, err := root.ExecuteContextC(ctx)
	if err == nil {
		printUpdateNotice(executed, d)
		return 0
	}
	if errors.Is(err, errPagerInterrupted) {
		return 0
	}
	code = 2
	var exit *ExitError
	if errors.As(err, &exit) {
		code = exit.Code
	}
	if ctx.Err() != nil {
		code = 130
		if exit == nil || exit.Code != 130 {
			err = &ExitError{130, "Interrupted."}
		}
	}
	if err.Error() != "" {
		err = requiredOptionError(executed, err)
		_, _ = fmt.Fprintln(stderr, output.MultilineText(err.Error()))
	}
	return code
}

// Timing is explicitly opt-in, independently of ordinary --verbose diagnostics.
func (d Dependencies) timingEnabled() bool {
	for _, category := range strings.Split(d.Getenv("AVR_DEBUG"), ",") {
		if strings.EqualFold(strings.TrimSpace(category), "timing") {
			return true
		}
	}
	return false
}
