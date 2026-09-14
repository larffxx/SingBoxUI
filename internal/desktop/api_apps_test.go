package desktop

import (
	"testing"

	"github.com/larffxx/singboxui/internal/domain/applications"
)

// The applications screen reads this payload directly, so what matters here is
// that an unsupported machine is an answer rather than a failure (ADR 011).

func TestAppsAPIListsTheCatalogOfTheMachine(t *testing.T) {
	h := newHarness(t, func(deps *Deps) {
		plat, ok := deps.Platform.(*fakePlatform)
		if !ok {
			t.Fatalf("the harness platform is %T", deps.Platform)
		}
		plat.apps = &fakeApplications{supported: true, items: []applications.Application{{
			Name:       "Telegram",
			BundleID:   "ru.keepcoder.Telegram",
			Path:       "/Applications/Telegram.app",
			Executable: "Telegram",
			MatchKey:   applications.MatchProcessPathRegex,
			MatchValue: `^/Applications/Telegram\.app/`,
		}}}
	})

	got := h.app.AppsAPI.ListApplications()
	if got.Error != nil {
		t.Fatalf("ListApplications reported %v", got.Error)
	}
	if !got.Apps.Supported {
		t.Fatal("Supported = false on a machine that lists applications")
	}
	if len(got.Apps.Applications) != 1 {
		t.Fatalf("Applications = %+v, want one entry", got.Apps.Applications)
	}
	entry := got.Apps.Applications[0]
	if entry.Name != "Telegram" || entry.MatchKey == "" || entry.MatchValue == "" {
		t.Errorf("entry = %+v, want a name and a condition to write into a rule", entry)
	}
}

func TestAppsAPIExplainsAMachineWithoutACatalog(t *testing.T) {
	h := newHarness(t, func(deps *Deps) {
		plat, ok := deps.Platform.(*fakePlatform)
		if !ok {
			t.Fatalf("the harness platform is %T", deps.Platform)
		}
		plat.apps = &fakeApplications{supported: false, reason: "this machine lists no applications"}
	})

	got := h.app.AppsAPI.ListApplications()
	if got.Error != nil {
		t.Fatalf("an unsupported machine is not a failure, got %v", got.Error)
	}
	if got.Apps.Supported {
		t.Error("Supported = true although the catalog lists nothing")
	}
	if got.Apps.Reason == "" {
		t.Error("the payload explains nothing")
	}
}
