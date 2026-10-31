package config

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// Store is shared with Python. All credentials belong to the caller's resolved host.
type Store struct{ Path string }

// Hosts preserves insertion order, including when promoting a new default.
type Hosts struct {
	DefaultHost string
	Entries     map[string]any
	Order       []string
}

func emptyHosts() Hosts { return Hosts{Entries: make(map[string]any)} }

// Read is a convenience for callers that have already loaded configuration.
func (s *Store) Read() Hosts {
	h, _ := s.ReadChecked()
	return h
}

// ReadChecked distinguishes an absent store from an unreadable one.
func (s *Store) ReadChecked() (Hosts, error) {
	data, err := os.ReadFile(s.Path)
	if errors.Is(err, os.ErrNotExist) {
		return emptyHosts(), nil
	}
	if err != nil {
		return emptyHosts(), fmt.Errorf("read credentials: %w", err)
	}
	return decodeHosts(data), nil
}

func decodeHosts(data []byte) Hosts {
	var root map[string]json.RawMessage
	if json.Unmarshal(data, &root) != nil {
		return emptyHosts()
	}
	d := json.NewDecoder(bytes.NewReader(root["hosts"]))
	d.UseNumber()
	tok, err := d.Token()
	if err != nil || tok != json.Delim('{') {
		return emptyHosts()
	}
	h := emptyHosts()
	for d.More() {
		tok, err = d.Token()
		if err != nil {
			return emptyHosts()
		}
		name, ok := tok.(string)
		if !ok {
			return emptyHosts()
		}
		var value any
		if d.Decode(&value) != nil {
			return emptyHosts()
		}
		if _, exists := h.Entries[name]; !exists {
			h.Order = append(h.Order, name)
		}
		h.Entries[name] = value
	}
	if _, err = d.Token(); err != nil {
		return emptyHosts()
	}
	_ = json.Unmarshal(root["default_host"], &h.DefaultHost)
	if _, ok := h.Entries[h.DefaultHost]; !ok {
		h.DefaultHost = ""
	}
	return h
}

func (h Hosts) Value(host, key string) string {
	entry, _ := h.Entries[host].(map[string]any)
	value, _ := entry[key].(string)
	return value
}

// Write uses a private temporary file and atomic replacement to avoid partial
// credential files. Existing per-host fields and insertion order are retained.
func (s *Store) Write(h Hosts) error {
	var b bytes.Buffer
	b.WriteString("{\"default_host\":")
	var def any
	if h.DefaultHost != "" {
		def = h.DefaultHost
	}
	data, err := json.Marshal(def)
	if err != nil {
		return err
	}
	b.Write(data)
	b.WriteString(",\"hosts\":{")
	for i, host := range h.Order {
		if i > 0 {
			b.WriteByte(',')
		}
		name, e := json.Marshal(host)
		if e != nil {
			return e
		}
		value, e := json.Marshal(h.Entries[host])
		if e != nil {
			return e
		}
		b.Write(name)
		b.WriteByte(':')
		b.Write(value)
	}
	b.WriteString("}}")
	var formatted bytes.Buffer
	if err := json.Indent(&formatted, b.Bytes(), "", "  "); err != nil {
		return err
	}
	formatted.WriteByte('\n')
	dir := filepath.Dir(s.Path)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return fmt.Errorf("create credentials directory: %w", err)
	}
	f, err := os.CreateTemp(dir, ".hosts-*")
	if err != nil {
		return fmt.Errorf("create credentials file: %w", err)
	}
	defer func() { _ = os.Remove(f.Name()) }()
	if _, err = io.Copy(f, &formatted); err != nil {
		_ = f.Close()
		return err
	}
	if err = f.Sync(); err != nil {
		_ = f.Close()
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	if err = os.Rename(f.Name(), s.Path); err != nil {
		return fmt.Errorf("save credentials: %w", err)
	}
	return nil
}

func (s *Store) Set(host, key, value string) error {
	h, err := s.ReadChecked()
	if err != nil {
		return err
	}
	entry, ok := h.Entries[host].(map[string]any)
	if !ok || entry == nil {
		if _, exists := h.Entries[host]; !exists {
			h.Order = append(h.Order, host)
		}
		entry = make(map[string]any)
		h.Entries[host] = entry
	}
	entry[key] = value
	if key == "token" && h.DefaultHost == "" {
		h.DefaultHost = host
	}
	return s.Write(h)
}

func (s *Store) Switch(host string) error {
	h, err := s.ReadChecked()
	if err != nil {
		return err
	}
	if _, ok := h.Entries[host]; !ok {
		return fmt.Errorf("no stored credentials for host %q", host)
	}
	h.DefaultHost = host
	return s.Write(h)
}

func (s *Store) Clear(host string) (bool, error) {
	h, err := s.ReadChecked()
	if err != nil {
		return false, err
	}
	if _, ok := h.Entries[host]; !ok {
		return false, nil
	}
	delete(h.Entries, host)
	order := make([]string, 0, len(h.Order))
	for _, item := range h.Order {
		if item != host {
			order = append(order, item)
		}
	}
	h.Order = order
	if len(order) == 0 {
		err := os.Remove(s.Path)
		if errors.Is(err, os.ErrNotExist) {
			err = nil
		}
		return true, err
	}
	if h.DefaultHost == host {
		h.DefaultHost = order[0]
	}
	return true, s.Write(h)
}

// ClearDefaultOrg removes only the active host's stored organization override.
// Tokens, unknown fields, host ordering and the selected default host survive.
func (s *Store) ClearDefaultOrg(host string) (bool, error) {
	h, err := s.ReadChecked()
	if err != nil {
		return false, err
	}
	entry, ok := h.Entries[host].(map[string]any)
	if !ok {
		return false, nil
	}
	if _, ok = entry["default_org"]; !ok {
		return false, nil
	}
	delete(entry, "default_org")
	return true, s.Write(h)
}
