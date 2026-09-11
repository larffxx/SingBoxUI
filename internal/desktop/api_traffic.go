package desktop

import (
	"github.com/larffxx/singboxui/internal/app/traffic"
	"github.com/larffxx/singboxui/internal/domain/apperr"
)

// TrafficAPI is the traffic facade bound to the frontend (spec §31, §45, §46).
//
// Live updates arrive as Wails events; this method exists so a freshly opened
// screen can render the current snapshot without waiting for the next tick.
type TrafficAPI struct{ app *App }

// TrafficPayload carries the latest traffic snapshot.
type TrafficPayload struct {
	Snapshot  traffic.Snapshot `json:"snapshot"`
	Collector bool             `json:"collectorRunning"`
	Error     *apperr.Error    `json:"error,omitempty"`
}

// GetTrafficSnapshot returns the latest snapshot and whether the collector is
// running. A stopped collector is a normal state, not an error.
func (a *TrafficAPI) GetTrafficSnapshot() TrafficPayload {
	return TrafficPayload{
		Snapshot:  a.app.traffic.Latest(),
		Collector: a.app.traffic.Running(),
	}
}
