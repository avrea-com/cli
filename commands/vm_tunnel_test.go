package commands

import (
	"context"
	"errors"
	"net"
	"slices"
	"testing"
	"time"
)

type vmPortListener struct {
	port   int
	closed bool
}

func (l *vmPortListener) Accept() (net.Conn, error) { return nil, errors.New("unused") }
func (l *vmPortListener) Close() error              { l.closed = true; return nil }
func (l *vmPortListener) Addr() net.Addr {
	return &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1), Port: l.port}
}

func TestVMForwardsReserveExplicitPortsBeforeAutoSelection(t *testing.T) {
	d, _, _ := testDeps(t)
	d = d.defaults()
	choices := []int{48080, 45000, 45000, 45001}
	listeners := []*vmPortListener{}
	d.Listen = func(_ context.Context, network, address string) (net.Listener, error) {
		if network != "tcp4" || address != "127.0.0.1:0" {
			t.Fatal(network, address)
		}
		if len(listeners) >= len(choices) {
			t.Fatal("port selection did not avoid collisions")
		}
		listener := &vmPortListener{port: choices[len(listeners)]}
		listeners = append(listeners, listener)
		return listener, nil
	}
	forwards, err := vmForwards(t.Context(), d, []string{"8080", "48080:3000", "5432"}, 0)
	if err != nil || !slices.Equal(forwards, []vmForward{{45000, 8080}, {48080, 3000}, {45001, 5432}}) {
		t.Fatal(forwards, err)
	}
	for _, listener := range listeners {
		if !listener.closed {
			t.Fatal("probe listener leaked")
		}
	}
}
func TestVMTunnelReadinessChecksAllPortsAndDeadline(t *testing.T) {
	listener, err := (&net.ListenConfig{}).Listen(t.Context(), "tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = listener.Close() }()
	port := listener.Addr().(*net.TCPAddr).Port
	d, _, _ := testDeps(t)
	d = d.defaults()
	proc := &vmTunnelProcess{done: make(chan struct{})}
	ready, err := vmTunnelListening(t.Context(), proc, []vmForward{{port, 8080}}, time.Second, d)
	if err != nil || !ready {
		t.Fatal(ready, err)
	}
	now := time.Unix(0, 0)
	d.Now = func() time.Time { return now }
	waits := 0
	d.Wait = func(_ context.Context, delay time.Duration) error { now = now.Add(delay); waits++; return nil }
	// Releasing a second ephemeral listener makes only that forward unavailable.
	second, err := (&net.ListenConfig{}).Listen(t.Context(), "tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	closedPort := second.Addr().(*net.TCPAddr).Port
	_ = second.Close()
	ready, err = vmTunnelListening(t.Context(), proc, []vmForward{{port, 8080}, {closedPort, 5432}}, time.Second, d)
	if err != nil || ready || waits != 4 {
		t.Fatal(ready, waits, err)
	}
	close(proc.done)
	ready, err = vmTunnelListening(t.Context(), proc, []vmForward{{port, 8080}}, time.Second, d)
	if err != nil || ready {
		t.Fatal("exited process reported ready", err)
	}
}
func TestVMDesktopClientSelectionAcrossHostPlatforms(t *testing.T) {
	for _, tc := range []struct {
		protocol, platform, guest string
		argv                      []string
		detaches                  bool
	}{
		{"rdp", "windows", "windows", []string{"mstsc", "/v:127.0.0.1:33389"}, false},
		{"rdp", "darwin", "linux", []string{"open", "rdp://full%20address=s:127.0.0.1:33389&username=s:runner"}, true},
		{"rdp", "linux", "linux", []string{"xfreerdp", "/v:127.0.0.1:33389", "/u:runner", "+clipboard", "/cert:ignore", "/gfx:rfx"}, false},
		{"rdp", "linux", "windows", []string{"xfreerdp", "/v:127.0.0.1:33389", "/u:runner", "+clipboard", "/cert:ignore"}, false},
		{"vnc", "darwin", "macos", []string{"open", "vnc://127.0.0.1:33389"}, true},
		{"vnc", "linux", "macos", nil, false},
		{"vnc", "windows", "macos", nil, false},
	} {
		args, detaches := vmDesktopLaunch(tc.protocol, "127.0.0.1:33389", "runner", tc.guest, tc.platform)
		if !slices.Equal(args, tc.argv) || detaches != tc.detaches {
			t.Fatal(tc, args, detaches)
		}
	}
}
