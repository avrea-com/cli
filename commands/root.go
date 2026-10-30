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
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

// Dependencies are explicit so admin can reuse commands without package globals.
type Dependencies struct {
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

func (d Dependencies) load() (config.Config, *config.Store, *client.Client, error) {
	store := d.Store
	if store == nil {
		var err error
		store, err = config.DefaultStore()
		if err != nil {
			return config.Config{}, nil, nil, err
		}
	}
	var cfg config.Config
	var err error
	if d.LoadConfig != nil {
		cfg, err = d.LoadConfig()
	} else {
		cfg, err = config.Load(store, d.Getenv)
	}
	if err != nil {
		return cfg, store, nil, err
	}
	agent := fmt.Sprintf("avr-cli/%s (%s %s; %s)", d.Version, runtime.GOOS, runtime.GOARCH, runtime.Version())
	api := client.New(cfg, d.HTTP, agent)
	if d.Verbose() {
		api.Trace = d.Err
	}
	if d.timingEnabled() {
		api.Timings = d.Err
	}
	return cfg, store, api, nil
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
		if isBrokenPipe(err) || errors.Is(err, io.ErrClosedPipe) {
			return &ExitError{Code: 0}
		}
		var exit *ExitError
		if errors.As(err, &exit) {
			return err
		}
		if errors.Is(err, context.Canceled) {
			return &ExitError{130, "Interrupted."}
		}
		return &ExitError{1, "Error: " + err.Error()}
	}
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
	root.AddCommand(NewAuth(d), NewOrg(d), NewRepo(d), NewRun(d), NewJob(d), NewWorkflow(d), NewPR(d), NewStatus(d), NewArtifact(d), NewCache(d), NewLog(d), NewSBOM(d), NewConfig(d), NewToken(d), NewSettings(d), NewAudit(d), NewHealth(d), NewBilling(d), NewFirewall(d), NewVM(d))
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
	cmd.AddCommand(NewLogin(d), NewLogout(d), newStatus(d), newSwitch(d), newGitCredential(d))
	return cmd
}

// Execute is the standalone entry boundary; embedded consumers can execute their
// own root and inspect ExitError without a library ever calling os.Exit.
func Execute(ctx context.Context, args []string, deps Dependencies) int {
	started := time.Now()
	d := deps.defaults()
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
func ExecuteRoot(ctx context.Context, root *cobra.Command, args []string, stderr io.Writer, dependencies ...Dependencies) int {
	if code, handled := executeCompletion(root, dependencies); handled {
		return code
	}
	if stderr == nil {
		stderr = os.Stderr
	}
	root.SetArgs(args)
	err := root.ExecuteContext(ctx)
	if err == nil || errors.Is(err, errPagerInterrupted) {
		return 0
	}
	code := 2
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
		_, _ = fmt.Fprintln(stderr, err)
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
