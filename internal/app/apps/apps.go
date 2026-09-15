// Package apps is the use case behind the applications screen: which programs
// the user can send through a chosen outbound, described so that a routing rule
// selects them by name instead of by address (ADR 011).
//
// Listing is all it does. Nothing is persisted here and no rule is written: the
// frontend turns a chosen application into a rule of the configuration draft,
// which then travels the ordinary revision flow.
package apps

import (
	"context"
	"log/slog"

	"github.com/larffxx/singboxui/internal/domain/apperr"
	"github.com/larffxx/singboxui/internal/domain/applications"
)

// Catalog is the operating system boundary this service depends on.
type Catalog interface {
	// Supported reports whether this machine can list its applications.
	Supported() bool
	// UnsupportedReason explains why not, when Supported is false.
	UnsupportedReason() string
	// List returns the installed applications, sorted by name.
	List() ([]applications.Application, error)
}

// Deps wires the service explicitly; nothing is global (spec §63).
type Deps struct {
	Catalog Catalog
	Logger  *slog.Logger
}

// List is the whole applications surface the UI renders.
//
// Unsupported is a normal answer on a platform that cannot offer the list, so
// it travels in the payload with the reason instead of as an error: the routing
// screen explains itself rather than showing a failure.
type List struct {
	Supported    bool                       `json:"supported"`
	Reason       string                     `json:"reason,omitempty"`
	Applications []applications.Application `json:"applications"`
}

// Service is the applications use case set.
type Service struct {
	deps Deps
}

// New builds the service.
func New(deps Deps) *Service {
	return &Service{deps: deps}
}

// List returns the applications of this machine, or the reason there are none
// to choose from.
func (s *Service) List(_ context.Context) (List, error) {
	const op = "apps.List"
	if !s.deps.Catalog.Supported() {
		return List{Supported: false, Reason: s.deps.Catalog.UnsupportedReason()}, nil
	}
	items, err := s.deps.Catalog.List()
	if err != nil {
		return List{}, apperr.Wrap(apperr.CodeInternal, op, "the installed applications cannot be listed", err)
	}
	if items == nil {
		items = []applications.Application{}
	}
	return List{Supported: true, Applications: items}, nil
}
