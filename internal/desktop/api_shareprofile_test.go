package desktop

import (
	"encoding/json"
	"strconv"
	"strings"
	"testing"

	"github.com/larffxx/singboxui/internal/domain/apperr"
	"github.com/larffxx/singboxui/internal/domain/profile"
)

// A vless link with a remark: the shape a provider hands out. The credentials are fake and are
// never asserted on.
const shareLinkVless = "vless://11111111-2222-3333-4444-555555555555@server.example.com:443" +
	"?type=tcp&security=reality&sni=www.example.org&fp=chrome&pbk=AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA" +
	"&sid=0123#Node%20One"

func TestCreateProfileFromShareLinksCreatesTheProfileAndItsRevision(t *testing.T) {
	h := newHarness(t)

	payload := h.app.ProfileAPI.CreateProfileFromShareLinks(CreateProfileFromShareLinksRequest{
		Links: shareLinkVless,
	})
	if payload.Error != nil {
		t.Fatalf("CreateProfileFromShareLinks() failed: %+v", payload.Error)
	}
	if payload.Profile.ID == "" {
		t.Fatal("the created profile has no id")
	}
	if payload.Kind != "vless" || payload.Count != 1 {
		t.Errorf("summary = kind %q, count %d, want one vless link", payload.Kind, payload.Count)
	}
	if payload.GeneratedName != "Node One" {
		t.Errorf("generated name = %q, want the link's remark", payload.GeneratedName)
	}
	// The name the user did not type is the link's remark, and the profile is readable.
	got := h.app.ProfileAPI.GetProfile(payload.Profile.ID)
	if got.Error != nil {
		t.Fatalf("GetProfile() failed: %+v", got.Error)
	}
	if got.Profile.Name != "Node One" {
		t.Errorf("profile name = %q, want the remark as the fallback", got.Profile.Name)
	}

	// The revision carries a configuration the link's server appears in, as JSON.
	revisions := h.app.ProfileAPI.ListRevisions(payload.Profile.ID, 10)
	if revisions.Error != nil {
		t.Fatalf("ListRevisions() failed: %+v", revisions.Error)
	}
	if len(revisions.Revisions) != 1 {
		t.Fatalf("revisions = %d, want the generated first one", len(revisions.Revisions))
	}
	if revisions.Revisions[0].Revision.Source != profile.SourceShareImport {
		t.Errorf("revision source = %q, want %q",
			revisions.Revisions[0].Revision.Source, profile.SourceShareImport)
	}
	document := map[string]any{}
	if err := json.Unmarshal([]byte(revisions.Revisions[0].Revision.ConfigJSON), &document); err != nil {
		t.Fatalf("the stored revision is not JSON: %v", err)
	}
	if !strings.Contains(revisions.Revisions[0].Revision.ConfigJSON, "server.example.com") {
		t.Error("the stored configuration does not carry the server from the link")
	}
}

func TestCreateProfileFromShareLinksLetsTheTypedNameWin(t *testing.T) {
	h := newHarness(t)
	payload := h.app.ProfileAPI.CreateProfileFromShareLinks(CreateProfileFromShareLinksRequest{
		Name:  "Домашний сервер",
		Links: shareLinkVless,
	})
	if payload.Error != nil {
		t.Fatalf("CreateProfileFromShareLinks() failed: %+v", payload.Error)
	}
	if payload.Profile.Name != "Домашний сервер" {
		t.Errorf("profile name = %q, want the typed name", payload.Profile.Name)
	}
}

func TestCreateProfileFromShareLinksReportsUnusableLines(t *testing.T) {
	h := newHarness(t)
	payload := h.app.ProfileAPI.CreateProfileFromShareLinks(CreateProfileFromShareLinksRequest{
		Links: "nonsense\n" + shareLinkVless,
	})
	if payload.Error != nil {
		t.Fatalf("CreateProfileFromShareLinks() failed: %+v", payload.Error)
	}
	if payload.Count != 1 {
		t.Errorf("count = %d, want the one usable link", payload.Count)
	}
	if len(payload.Warnings) == 0 {
		t.Fatal("no warning was reported for the unusable line")
	}
	if !strings.Contains(strings.Join(payload.Warnings, "\n"), "line 1") {
		t.Errorf("warnings = %v, want the source line of the skipped input", payload.Warnings)
	}
}

func TestCreateProfileFromShareLinksRefusesAnUnusablePaste(t *testing.T) {
	h := newHarness(t)
	payload := h.app.ProfileAPI.CreateProfileFromShareLinks(CreateProfileFromShareLinksRequest{
		Links: "https://example.com/subscription",
	})
	if payload.Error == nil {
		t.Fatal("CreateProfileFromShareLinks() succeeded for a paste with no link in it")
	}
	if payload.Error.Code != apperr.CodeShareLinkInvalid {
		t.Errorf("code = %s, want %s", payload.Error.Code, apperr.CodeShareLinkInvalid)
	}
	// Nothing may be left behind: the dialog stays open on the paste.
	list := h.app.ProfileAPI.ListProfiles()
	if list.Error != nil {
		t.Fatalf("ListProfiles() failed: %+v", list.Error)
	}
	if len(list.Profiles) != 0 {
		t.Errorf("profiles = %d, want none", len(list.Profiles))
	}
}

func TestCreateProfileFromShareLinksRefusesAnUnknownBase(t *testing.T) {
	h := newHarness(t)
	payload := h.app.ProfileAPI.CreateProfileFromShareLinks(CreateProfileFromShareLinksRequest{
		Links: shareLinkVless,
		Base:  "wireguard",
	})
	if payload.Error == nil {
		t.Fatal("CreateProfileFromShareLinks() accepted an unknown base")
	}
	if payload.Error.Code != apperr.CodeInvalidArgument {
		t.Errorf("code = %s, want %s", payload.Error.Code, apperr.CodeInvalidArgument)
	}
}

func TestListShareLinkBasesDeclaresWhatCanBeGenerated(t *testing.T) {
	h := newHarness(t)
	payload := h.app.ProfileAPI.ListShareLinkBases()
	if payload.Error != nil {
		t.Fatalf("ListShareLinkBases() failed: %+v", payload.Error)
	}
	if len(payload.Bases) < 2 {
		t.Fatalf("bases = %d, want the TUN and the local one", len(payload.Bases))
	}
	for _, base := range payload.Bases {
		if base.ID == "" || base.Name == "" {
			t.Errorf("base %+v is incomplete", base)
		}
	}
	if !payload.Bases[0].RequiresPrivilege {
		t.Error("the first base is the default and is expected to be the TUN one")
	}
	// Every declared base must actually generate: the dialog offers exactly what comes back.
	// Profile names are unique in the store, so each base creates under a name of its own.
	for i, base := range payload.Bases {
		created := h.app.ProfileAPI.CreateProfileFromShareLinks(CreateProfileFromShareLinksRequest{
			Name:  "base-" + strconv.Itoa(i),
			Links: shareLinkVless,
			Base:  base.ID,
		})
		if created.Error != nil {
			t.Errorf("base %s could not generate a profile: %+v", base.ID, created.Error)
		}
	}
}
