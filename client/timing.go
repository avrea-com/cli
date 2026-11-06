package client

import (
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptrace"
	"sync"
	"time"
)

// TimingEvent records an offset from the start of one HTTP operation. Events
// retain concurrent connection attempts rather than adding overlapping durations.
type TimingEvent struct {
	Name         string  `json:"name"`
	Milliseconds float64 `json:"ms"`
	Attempt      int     `json:"attempt,omitempty"`
	Failed       bool    `json:"failed,omitempty"`
}

type requestTiming struct {
	mu       sync.Mutex
	start    time.Time
	done     bool
	attempts map[string]int
	events   []TimingEvent
	status   int
	protocol string
	reused   bool
}

func newRequestTiming() *requestTiming {
	return &requestTiming{start: time.Now(), attempts: make(map[string]int)}
}

func (t *requestTiming) addLocked(name string, attempt int, failed bool) {
	if !t.done {
		t.events = append(t.events, TimingEvent{Name: name, Milliseconds: float64(time.Since(t.start)) / float64(time.Millisecond), Attempt: attempt, Failed: failed})
	}
}

func (t *requestTiming) mark(name string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.addLocked(name, 0, false)
}

func (t *requestTiming) hooks() *httptrace.ClientTrace {
	return &httptrace.ClientTrace{
		GetConn:  func(string) { t.mark("get_connection") },
		DNSStart: func(httptrace.DNSStartInfo) { t.mark("dns_start") },
		DNSDone: func(info httptrace.DNSDoneInfo) {
			t.mu.Lock()
			defer t.mu.Unlock()
			t.addLocked("dns_done", 0, info.Err != nil)
		},
		ConnectStart: func(network, address string) {
			t.mu.Lock()
			defer t.mu.Unlock()
			// Addresses are used only to correlate callbacks; never emitted.
			key := network + "\x00" + address
			id := len(t.attempts) + 1
			t.attempts[key] = id
			t.addLocked("connect_start", id, false)
		},
		ConnectDone: func(network, address string, err error) {
			t.mu.Lock()
			defer t.mu.Unlock()
			t.addLocked("connect_done", t.attempts[network+"\x00"+address], err != nil)
		},
		TLSHandshakeStart: func() { t.mark("tls_start") },
		TLSHandshakeDone: func(_ tls.ConnectionState, err error) {
			t.mu.Lock()
			defer t.mu.Unlock()
			t.addLocked("tls_done", 0, err != nil)
		},
		GotConn: func(info httptrace.GotConnInfo) {
			t.mu.Lock()
			defer t.mu.Unlock()
			t.reused = info.Reused
			t.addLocked("got_connection", 0, false)
		},
		WroteRequest: func(info httptrace.WroteRequestInfo) {
			t.mu.Lock()
			defer t.mu.Unlock()
			t.addLocked("request_written", 0, info.Err != nil)
		},
		GotFirstResponseByte: func() { t.mark("first_response_byte") },
	}
}

func (t *requestTiming) response(resp *http.Response) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if resp != nil {
		t.status = resp.StatusCode
		t.protocol = resp.Proto
	}
	t.addLocked("response_headers", 0, resp == nil)
}

func (t *requestTiming) finish(w io.Writer) {
	t.mu.Lock()
	t.addLocked("complete", 0, false)
	t.done = true
	snapshot := struct {
		Kind             string        `json:"kind"`
		Duration         float64       `json:"duration_ms"`
		Status           int           `json:"status"`
		Protocol         string        `json:"protocol"`
		ConnectionReused bool          `json:"connection_reused"`
		Events           []TimingEvent `json:"events"`
	}{"http", float64(time.Since(t.start)) / float64(time.Millisecond), t.status, t.protocol, t.reused, append([]TimingEvent(nil), t.events...)}
	t.mu.Unlock()
	// Fixed event names, timings and protocol metadata only: no URL, hostname,
	// headers, errors, cookies, credentials, or response content.
	data, err := json.Marshal(snapshot)
	if err == nil {
		_, _ = fmt.Fprintf(w, "avr-timing %s\n", data)
	}
}
