package desktop

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"unicode/utf16"

	"github.com/larffxx/singboxui/internal/domain/apperr"
	"github.com/larffxx/singboxui/internal/domain/profile"
	wruntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

const sampleConfig = `{
  "log": { "level": "warn" },
  "inbounds": [{ "type": "tun", "tag": "tun-in", "interface_name": "utun9", "auto_route": true }],
  "outbounds": [{ "type": "direct", "tag": "direct" }],
  "route": { "final": "direct", "rules": [], "default_domain_resolver": "local" }
}`

func writeConfigFile(t *testing.T, name, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("os.WriteFile(%s): %v", path, err)
	}
	return path
}

// jsonEqual compares two documents by structure: the import is allowed to
// re-indent, but no key or value may change (spec §85).
func jsonEqual(t *testing.T, left, right string) bool {
	t.Helper()
	var a, b any
	if err := json.Unmarshal([]byte(left), &a); err != nil {
		t.Fatalf("json.Unmarshal(left): %v", err)
	}
	if err := json.Unmarshal([]byte(right), &b); err != nil {
		t.Fatalf("json.Unmarshal(right): %v", err)
	}
	return reflect.DeepEqual(a, b)
}

func TestImportConfigFileCreatesAProfileFromTheChosenFile(t *testing.T) {
	h := newHarness(t)
	path := writeConfigFile(t, "my-config.json", sampleConfig)

	payload := h.app.ConfigAPI.ImportConfigFile(ImportConfigFileRequest{Path: path})
	if payload.Error != nil {
		t.Fatalf("ImportConfigFile(%q) failed: %+v", path, payload.Error)
	}
	if payload.Profile.ID == "" {
		t.Fatal("ImportConfigFile returned an empty profile id")
	}
	// The name comes from the file, so importing needs no typing.
	if payload.Profile.Name != "my-config" {
		t.Fatalf("profile name = %q, want %q", payload.Profile.Name, "my-config")
	}
	if !strings.Contains(payload.Profile.Description, path) {
		t.Fatalf("profile description %q does not mention the source path", payload.Profile.Description)
	}

	draft := h.app.ConfigAPI.GetDraft(payload.Profile.ID)
	if draft.Error != nil {
		t.Fatalf("GetDraft: %+v", draft.Error)
	}
	if !jsonEqual(t, draft.Draft.ConfigJSON, sampleConfig) {
		t.Fatalf("imported configuration differs from the file:\n%s", draft.Draft.ConfigJSON)
	}
	if draft.Draft.BaseSource != profile.SourceImport {
		t.Fatalf("base revision source = %q, want %q", draft.Draft.BaseSource, profile.SourceImport)
	}

	// The file the user pointed at is only ever read.
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("os.ReadFile(%s): %v", path, err)
	}
	if string(after) != sampleConfig {
		t.Fatalf("the imported file changed on disk:\n%s", string(after))
	}
}

func TestImportConfigFileStripsAUTF8BOM(t *testing.T) {
	h := newHarness(t)
	// Windows editors write a BOM; the parser rejects it, so the import must.
	path := writeConfigFile(t, "bom.json", "\ufeff"+sampleConfig)

	payload := h.app.ConfigAPI.ImportConfigFile(ImportConfigFileRequest{Path: path})
	if payload.Error != nil {
		t.Fatalf("ImportConfigFile on a BOM file failed: %+v", payload.Error)
	}
	draft := h.app.ConfigAPI.GetDraft(payload.Profile.ID)
	if draft.Error != nil {
		t.Fatalf("GetDraft: %+v", draft.Error)
	}
	if strings.HasPrefix(draft.Draft.ConfigJSON, "\ufeff") {
		t.Fatal("the byte order mark survived the import")
	}
	if !jsonEqual(t, draft.Draft.ConfigJSON, sampleConfig) {
		t.Fatalf("configuration after a BOM import:\n%s", draft.Draft.ConfigJSON)
	}
}

func TestImportConfigFileReadsAUTF16FileWrittenOnWindows(t *testing.T) {
	h := newHarness(t)
	// PowerShell redirection and Notepad's "Unicode" encoding both write UTF-16
	// with a BOM, and neither is JSON as far as encoding/json is concerned.
	raw := []byte{0xFF, 0xFE}
	for _, unit := range utf16.Encode([]rune(sampleConfig)) {
		raw = binary.LittleEndian.AppendUint16(raw, unit)
	}
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatalf("os.WriteFile(%s): %v", path, err)
	}

	payload := h.app.ConfigAPI.ImportConfigFile(ImportConfigFileRequest{Path: path})
	if payload.Error != nil {
		t.Fatalf("ImportConfigFile on a UTF-16 file failed: %+v", payload.Error)
	}
	draft := h.app.ConfigAPI.GetDraft(payload.Profile.ID)
	if draft.Error != nil {
		t.Fatalf("GetDraft: %+v", draft.Error)
	}
	if !jsonEqual(t, draft.Draft.ConfigJSON, sampleConfig) {
		t.Fatalf("configuration after a UTF-16 import:\n%s", draft.Draft.ConfigJSON)
	}
}

func TestImportConfigFileKeepsTheNameTheUserTyped(t *testing.T) {
	h := newHarness(t)
	path := writeConfigFile(t, "config.json", sampleConfig)

	payload := h.app.ConfigAPI.ImportConfigFile(ImportConfigFileRequest{Path: path, Name: "  Рабочий  "})
	if payload.Error != nil {
		t.Fatalf("ImportConfigFile failed: %+v", payload.Error)
	}
	if payload.Profile.Name != "Рабочий" {
		t.Fatalf("profile name = %q, want %q", payload.Profile.Name, "Рабочий")
	}
}

func TestImportConfigFileRejectsABadPath(t *testing.T) {
	h := newHarness(t)
	missing := filepath.Join(t.TempDir(), "nope.json")

	cases := []struct {
		name string
		req  ImportConfigFileRequest
		code apperr.Code
	}{
		{"an empty path", ImportConfigFileRequest{Path: "   "}, apperr.CodeInvalidArgument},
		{"a file that is not there", ImportConfigFileRequest{Path: missing}, apperr.CodeNotFound},
		{"a directory", ImportConfigFileRequest{Path: t.TempDir()}, apperr.CodeInvalidArgument},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			payload := h.app.ConfigAPI.ImportConfigFile(tc.req)
			if codeOf(payload.Error) != tc.code {
				t.Fatalf("code = %q, want %q (%+v)", codeOf(payload.Error), tc.code, payload.Error)
			}
			if payload.Profile.ID != "" {
				t.Fatalf("a rejected import still created profile %q", payload.Profile.ID)
			}
		})
	}
}

func TestImportConfigFileLeavesNothingBehindWhenTheJSONIsInvalid(t *testing.T) {
	h := newHarness(t)
	before := len(h.app.ProfileAPI.ListProfiles().Profiles)
	path := writeConfigFile(t, "broken.json", "{\n  \"log\": ,\n}")

	payload := h.app.ConfigAPI.ImportConfigFile(ImportConfigFileRequest{Path: path})
	if codeOf(payload.Error) != apperr.CodeConfigInvalid {
		t.Fatalf("code = %q, want %q (%+v)", codeOf(payload.Error), apperr.CodeConfigInvalid, payload.Error)
	}
	if after := len(h.app.ProfileAPI.ListProfiles().Profiles); after != before {
		t.Fatalf("profile count changed from %d to %d after a failed import", before, after)
	}
	// A rejected import must not touch the file either.
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("os.ReadFile(%s): %v", path, err)
	}
	if string(after) != "{\n  \"log\": ,\n}" {
		t.Fatalf("the rejected file changed on disk:\n%s", string(after))
	}
}

// The Wails file dialog needs the startup context: the app's own context is
// detached from Wails and carries no dialog handler.
func TestPickConfigFileUsesTheStartupContext(t *testing.T) {
	type ctxKey string
	var got context.Context
	h := newHarness(t, func(d *Deps) {
		d.OpenFileDialog = func(ctx context.Context, _ wruntime.OpenDialogOptions) (string, error) {
			got = ctx
			return "/tmp/config.json", nil
		}
	})
	startup := context.WithValue(context.Background(), ctxKey("wails"), true)
	h.app.OnStartup(startup)

	if payload := h.app.ConfigAPI.PickConfigFile(); payload.Error != nil {
		t.Fatalf("PickConfigFile: %+v", payload.Error)
	}
	if got != startup {
		t.Fatalf("the picker got %v, want the startup context %v", got, startup)
	}
}

// A dialog without a handler panics inside the Wails runtime; the app must
// survive that and tell the user the picker is unavailable.
func TestPickConfigFileSurvivesAPanickingPicker(t *testing.T) {
	h := newHarness(t, func(d *Deps) {
		d.OpenFileDialog = func(context.Context, wruntime.OpenDialogOptions) (string, error) {
			panic("interface conversion: interface {} is nil, not frontend.Frontend")
		}
	})

	payload := h.app.ConfigAPI.PickConfigFile()
	if payload.Error == nil {
		t.Fatal("a panicking picker reported success")
	}
	if payload.Error.Code != apperr.CodeInternal {
		t.Fatalf("error code = %s, want %s", payload.Error.Code, apperr.CodeInternal)
	}
	if !strings.Contains(payload.Error.Message, "frontend.Frontend") {
		t.Fatalf("error message = %q, want the panic text", payload.Error.Message)
	}
}

func TestPickConfigFileReturnsTheChosenPath(t *testing.T) {
	var options wruntime.OpenDialogOptions
	h := newHarness(t, func(d *Deps) {
		d.OpenFileDialog = func(_ context.Context, opts wruntime.OpenDialogOptions) (string, error) {
			options = opts
			return " /Users/test/Downloads/config.json ", nil
		}
	})

	payload := h.app.ConfigAPI.PickConfigFile()
	if payload.Error != nil {
		t.Fatalf("PickConfigFile failed: %+v", payload.Error)
	}
	if payload.Path != "/Users/test/Downloads/config.json" {
		t.Fatalf("path = %q, want the trimmed choice", payload.Path)
	}
	if payload.Canceled {
		t.Fatal("a chosen path was reported as canceled")
	}
	if len(options.Filters) == 0 || options.Filters[0].Pattern != "*.json" {
		t.Fatalf("the picker does not offer JSON files first: %+v", options.Filters)
	}
}

func TestPickConfigFileTreatsADismissedDialogAsCanceled(t *testing.T) {
	h := newHarness(t, func(d *Deps) {
		d.OpenFileDialog = func(context.Context, wruntime.OpenDialogOptions) (string, error) {
			return "", nil
		}
	})

	payload := h.app.ConfigAPI.PickConfigFile()
	if payload.Error != nil {
		t.Fatalf("dismissing the picker reported an error: %+v", payload.Error)
	}
	if !payload.Canceled || payload.Path != "" {
		t.Fatalf("payload = %+v, want canceled with no path", payload)
	}
}

func TestPickConfigFileReportsAPickerFailure(t *testing.T) {
	h := newHarness(t, func(d *Deps) {
		d.OpenFileDialog = func(context.Context, wruntime.OpenDialogOptions) (string, error) {
			return "", errors.New("no window is available")
		}
	})

	payload := h.app.ConfigAPI.PickConfigFile()
	if codeOf(payload.Error) != apperr.CodeInternal {
		t.Fatalf("code = %q, want %q", codeOf(payload.Error), apperr.CodeInternal)
	}
	if payload.Path != "" || payload.Canceled {
		t.Fatalf("a failed picker still reported a path: %+v", payload)
	}
}

func TestImportProfileNameComesFromTheFileName(t *testing.T) {
	cases := map[string]string{
		"/etc/sing-box/config.json": "config",
		"config.with-ipv6-fix.json": "config.with-ipv6-fix",
		"/tmp/backup.tar.gz":        "backup.tar",
		"/tmp/no-extension":         "no-extension",
		".json":                     "Imported configuration",
		"/tmp/":                     "tmp",
	}
	for path, want := range cases {
		if got := importProfileName(path); got != want {
			t.Fatalf("importProfileName(%q) = %q, want %q", path, got, want)
		}
	}
}
