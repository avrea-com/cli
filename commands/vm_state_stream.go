package commands

import (
	"context"
	"encoding/json"
	"time"

	"github.com/avrea-com/cli/client"
)

type vmStateNotifications struct {
	api         *client.Client
	path        string
	d           Dependencies
	stream      *client.ObservabilityStream
	unavailable bool
}

func (s *vmStateNotifications) Close() {
	if s.stream != nil {
		s.stream.Close()
	}
}

func (s *vmStateNotifications) Wait(ctx context.Context, timeout time.Duration) error {
	started := s.d.Now()
	deadline := started.Add(timeout)
	if !s.unavailable {
		if s.stream == nil {
			var err error
			s.stream, err = s.api.OpenObservability(ctx, s.path+"/observability/stream", "status", timeout)
			if err != nil {
				s.unavailable = true
			}
		}
		for !s.unavailable {
			remaining := deadline.Sub(s.d.Now())
			if remaining <= 0 {
				return ctx.Err()
			}
			raw, err := s.stream.Next(ctx, remaining)
			if ctx.Err() != nil {
				return ctx.Err()
			}
			if err != nil {
				s.unavailable = true
				break
			}
			if raw == nil {
				return nil
			}
			var frame any
			if json.Unmarshal(raw, &frame) != nil {
				s.unavailable = true
				break
			}
			value, ok := frame.(map[string]any)
			if !ok {
				continue
			}
			if value["type"] == "changed" && (value["signal"] == nil || value["signal"] == "status") {
				floor := min(started.Add(time.Second).Sub(s.d.Now()), deadline.Sub(s.d.Now()))
				if floor > 0 {
					return s.d.Wait(ctx, floor)
				}
				return ctx.Err()
			}
			if value["type"] == "error" {
				s.unavailable = true
			}
		}
		if s.unavailable {
			s.Close()
		}
	}
	if remaining := deadline.Sub(s.d.Now()); remaining > 0 {
		return s.d.Wait(ctx, remaining)
	}
	return ctx.Err()
}
