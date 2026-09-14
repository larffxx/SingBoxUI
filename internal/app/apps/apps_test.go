package apps

import (
	"context"
	"errors"
	"testing"

	"github.com/larffxx/singboxui/internal/domain/apperr"
	"github.com/larffxx/singboxui/internal/domain/applications"
)

// stubCatalog is the operating-system boundary the service is tested against.
type stubCatalog struct {
	supported bool
	reason    string
	items     []applications.Application
	err       error
}

func (c *stubCatalog) Supported() bool { return c.supported }

func (c *stubCatalog) UnsupportedReason() string { return c.reason }

func (c *stubCatalog) List() ([]applications.Application, error) {
	if c.err != nil {
		return nil, c.err
	}
	return c.items, nil
}

func TestListReturnsTheApplicationsOfTheMachine(t *testing.T) {
	service := New(Deps{Catalog: &stubCatalog{
		supported: true,
		items: []applications.Application{{
			Name:             "Telegram",
			BundleID:         "ru.keepcoder.Telegram",
			Path:             "/Applications/Telegram.app",
			Executable:       "Telegram",
			ProcessPathRegex: `^/Applications/Telegram\.app/`,
		}},
	}})

	got, err := service.List(context.Background())
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if !got.Supported {
		t.Fatal("Supported = false, want true")
	}
	if len(got.Applications) != 1 || got.Applications[0].Name != "Telegram" {
		t.Fatalf("Applications = %+v, want Telegram", got.Applications)
	}
	if got.Applications[0].ProcessPathRegex == "" {
		t.Error("the entry carries no condition to write into a rule")
	}
}

func TestListExplainsAnUnsupportedMachine(t *testing.T) {
	// A platform that cannot list applications is a normal answer, not a
	// failure: the routing screen shows the reason and the rest keeps working.
	service := New(Deps{Catalog: &stubCatalog{supported: false, reason: "не поддерживается"}})

	got, err := service.List(context.Background())
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if got.Supported {
		t.Error("Supported = true on a platform that cannot list applications")
	}
	if got.Reason == "" {
		t.Error("an unsupported machine comes back without a reason")
	}
	if got.Applications != nil {
		t.Errorf("Applications = %+v, want none", got.Applications)
	}
}

func TestListReportsAFailureAsAnError(t *testing.T) {
	service := New(Deps{Catalog: &stubCatalog{supported: true, err: errors.New("boom")}})

	_, err := service.List(context.Background())
	if err == nil {
		t.Fatal("List succeeded although the catalog failed")
	}
	if !apperr.IsCode(err, apperr.CodeInternal) {
		t.Errorf("List = %v, want an internal error", err)
	}
}
