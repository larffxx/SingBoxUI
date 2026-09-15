package config

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// This file covers the configuration helpers the UI drives directly: the
// revision/draft diff (spec §56), the legacy prototype import (spec §65) and the
// Clash API patch used for traffic monitoring (spec §30, §36). They are pure
// enough to test without a sing-box binary, which keeps the suite fast.

func TestDiffJSONClassifiesLinesAndIsIdenticalOnlyWhenNothingChanged(t *testing.T) {
	base := "{\n  \"log\": {\n    \"level\": \"info\"\n  },\n  \"outbounds\": []\n}"
	changed := "{\n  \"log\": {\n    \"level\": \"debug\"\n  },\n  \"outbounds\": []\n}"

	tests := []struct {
		name        string
		left        string
		right       string
		wantAdded   int
		wantRemoved int
		wantSame    bool
		wantTexts   []string
	}{
		{
			name:      "identical documents",
			left:      base,
			right:     base,
			wantSame:  true,
			wantTexts: nil,
		},
		{
			name:        "a single changed line",
			left:        base,
			right:       changed,
			wantAdded:   1,
			wantRemoved: 1,
			// Diff lines carry their original indentation so the view can be
			// rendered verbatim.
			wantTexts: []string{`    "level": "debug"`, `    "level": "info"`},
		},
		{
			name:        "an added line only",
			left:        "{\n}",
			right:       "{\n  \"log\": {}\n}",
			wantAdded:   1,
			wantRemoved: 0,
			wantTexts:   []string{`  "log": {}`},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			diff := DiffJSON(tt.left, tt.right)
			if diff.Identical != tt.wantSame {
				t.Errorf("Identical = %v, want %v", diff.Identical, tt.wantSame)
			}
			if diff.Added != tt.wantAdded || diff.Removed != tt.wantRemoved {
				t.Errorf("added/removed = %d/%d, want %d/%d", diff.Added, diff.Removed, tt.wantAdded, tt.wantRemoved)
			}
			if tt.wantSame && (diff.Added != 0 || diff.Removed != 0) {
				t.Errorf("an identical diff counted %d added and %d removed lines", diff.Added, diff.Removed)
			}
			for _, text := range tt.wantTexts {
				if !diffHasText(diff, text) {
					t.Errorf("diff does not mention %q: %+v", text, diff.Lines)
				}
			}
			// Every line must be attributed to at least one side and numbered
			// from 1, otherwise the side-by-side view cannot be rendered.
			for _, line := range diff.Lines {
				if line.Text == "" {
					t.Errorf("a diff line has no text: %+v", line)
				}
				switch line.Kind {
				case DiffSame:
					if line.Left == 0 || line.Right == 0 {
						t.Errorf("unchanged line %q is missing a side: %+v", line.Text, line)
					}
				case DiffAdded:
					if line.Left != 0 || line.Right == 0 {
						t.Errorf("added line %q has the wrong sides: %+v", line.Text, line)
					}
				case DiffRemoved:
					if line.Right != 0 || line.Left == 0 {
						t.Errorf("removed line %q has the wrong sides: %+v", line.Text, line)
					}
				default:
					t.Errorf("unknown diff kind %q", line.Kind)
				}
			}
		})
	}
}

func diffHasText(diff Diff, text string) bool {
	for _, line := range diff.Lines {
		if line.Text == text {
			return true
		}
	}
	return false
}

func TestCompareNormalisesBothSidesAndLabelsThem(t *testing.T) {
	h := newHarness(t)

	// The two sides differ only in whitespace, so the diff must be empty after
	// normalisation: a re-formatted file is not a change to the user.
	diff, err := h.svc.Compare(
		`{"log":{"level":"info"},"outbounds":[]}`,
		"{\n  \"log\": {\n    \"level\": \"info\"\n  },\n  \"outbounds\": []\n}",
		"draft", "revision 3",
	)
	if err != nil {
		t.Fatalf("Compare: %v", err)
	}
	if !diff.Identical {
		t.Errorf("re-formatted configuration reported as changed: %+v", diff.Lines)
	}
	if diff.Left != "draft" || diff.Right != "revision 3" {
		t.Errorf("labels = %q/%q, want draft/revision 3", diff.Left, diff.Right)
	}

	if _, err := h.svc.Compare("not json", "{}", "draft", "revision"); err == nil {
		t.Error("comparing an invalid document succeeded, want an error")
	}
	if _, err := h.svc.Compare("   ", "{}", "draft", "revision"); err == nil {
		t.Error("comparing an empty document succeeded, want an error")
	}
}

func TestCompareRevisionsUsesHumanLabels(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()

	if _, err := h.svc.SaveRevision(ctx, SaveInput{ProfileID: "p-1", ConfigJSON: configA, Comment: "a", Apply: true}); err != nil {
		t.Fatalf("saving the first revision: %v", err)
	}
	if _, err := h.svc.SaveRevision(ctx, SaveInput{ProfileID: "p-1", ConfigJSON: configB, Comment: "b"}); err != nil {
		t.Fatalf("saving the second revision: %v", err)
	}
	views, err := h.svc.ListRevisions(ctx, "p-1", 2)
	if err != nil {
		t.Fatalf("ListRevisions: %v", err)
	}
	if len(views) != 2 {
		t.Fatalf("ListRevisions returned %d revisions, want 2", len(views))
	}

	diff, err := h.svc.CompareRevisions(views[1], views[0])
	if err != nil {
		t.Fatalf("CompareRevisions: %v", err)
	}
	wantLeft, wantRight := revisionLabel(views[1].Revision), revisionLabel(views[0].Revision)
	if diff.Left != wantLeft || diff.Right != wantRight {
		t.Errorf("labels = %q/%q, want %q/%q", diff.Left, diff.Right, wantLeft, wantRight)
	}
	if diff.Left == diff.Right {
		t.Errorf("both sides share the label %q", diff.Left)
	}
	if diff.Added == 0 && diff.Removed == 0 {
		t.Errorf("two different revisions produced an empty diff: %+v", diff.Lines)
	}

	withActive, err := h.svc.CompareWithActive(configA, views[0])
	if err != nil {
		t.Fatalf("CompareWithActive: %v", err)
	}
	if withActive.Left == "" || withActive.Right == "" {
		t.Errorf("CompareWithActive left the labels empty: %+v", withActive)
	}
}

func TestSetClashAPIEnabledPatchesAndRemovesTheEndpoint(t *testing.T) {
	raw := `{"log":{"level":"info"},"inbounds":[{"type":"tun","tag":"tun-in"}],"outbounds":[]}`

	enabled, err := SetClashAPIEnabled([]byte(raw), true, "", "")
	if err != nil {
		t.Fatalf("enabling the Clash API: %v", err)
	}
	api := ClashAPIFromConfig(enabled)
	if !api.Enabled {
		t.Fatal("the Clash API was not enabled")
	}
	if api.BaseURL != "http://127.0.0.1:9090" {
		t.Errorf("BaseURL = %q, want the sing-box default controller", api.BaseURL)
	}
	if api.Secret != "" {
		t.Errorf("Secret = %q, want empty when none was requested", api.Secret)
	}

	// Unknown fields must survive the patch (spec §36).
	var patched map[string]any
	if err := json.Unmarshal(enabled, &patched); err != nil {
		t.Fatalf("the patched configuration is not valid JSON: %v", err)
	}
	if _, ok := patched["inbounds"]; !ok {
		t.Error("the patch dropped the inbound configuration")
	}

	withSecret, err := SetClashAPIEnabled(enabled, true, "127.0.0.1:9191", "s3cret")
	if err != nil {
		t.Fatalf("re-enabling the Clash API with a secret: %v", err)
	}
	api = ClashAPIFromConfig(withSecret)
	if api.BaseURL != "http://127.0.0.1:9191" || api.Secret != "s3cret" {
		t.Errorf("api = %+v, want the patched controller and secret", api)
	}
	if strings.Contains(string(withSecret), `"secret"`) == false {
		t.Error("the secret was not written into the configuration")
	}

	disabled, err := SetClashAPIEnabled(withSecret, false, "", "")
	if err != nil {
		t.Fatalf("disabling the Clash API: %v", err)
	}
	if api := ClashAPIFromConfig(disabled); api.Enabled {
		t.Errorf("the Clash API is still enabled: %+v", api)
	}
	var after map[string]any
	if err := json.Unmarshal(disabled, &after); err != nil {
		t.Fatalf("the configuration is not valid JSON after disabling: %v", err)
	}
	if _, ok := after["experimental"]; ok {
		t.Error("disabling left an empty experimental block behind")
	}
	if _, ok := after["log"]; !ok {
		t.Error("disabling dropped unrelated configuration")
	}

	if _, err := SetClashAPIEnabled([]byte("not json"), true, "", ""); err == nil {
		t.Error("patching invalid JSON succeeded, want an error")
	}
	if api := ClashAPIFromConfig([]byte("not json")); api.Enabled {
		t.Error("an unparsable configuration reported an enabled Clash API")
	}
}

func TestDetectLegacyReportsThePrototypeFilesWithoutModifyingThem(t *testing.T) {
	h := newHarness(t)
	dir := t.TempDir()

	configJSON := `{"log":{"level":"info"},"outbounds":[{"type":"direct","tag":"direct"}]}`
	writeFile(t, filepath.Join(dir, "config.json"), configJSON)
	writeFile(t, filepath.Join(dir, "ui-settings.json"), `{"autoConnect":true}`)
	if err := os.MkdirAll(filepath.Join(dir, "bin"), 0o755); err != nil {
		t.Fatalf("creating bin: %v", err)
	}
	writeFile(t, filepath.Join(dir, "bin", "sing-box"), "binary")
	before := statAll(t, dir)

	candidate := h.svc.DetectLegacy(dir)
	if !candidate.Found || candidate.ConfigPath != filepath.Join(dir, "config.json") {
		t.Fatalf("candidate = %+v, want the working directory configuration", candidate)
	}
	if !candidate.Valid {
		t.Errorf("the legacy configuration was reported invalid: %+v", candidate.Warnings)
	}
	if candidate.SettingsPath == "" || candidate.AutoConnect == nil || !*candidate.AutoConnect {
		t.Errorf("the prototype settings were not read: %+v", candidate)
	}
	if candidate.BinaryPath == "" {
		t.Error("the prototype binary was not detected")
	}
	if candidate.Size != len(configJSON) {
		t.Errorf("Size = %d, want %d", candidate.Size, len(configJSON))
	}
	if candidate.Modified.IsZero() {
		t.Error("Modified is zero, want the file modification time")
	}

	imported, err := h.svc.LegacyConfigJSON(candidate)
	if err != nil {
		t.Fatalf("LegacyConfigJSON: %v", err)
	}
	if !strings.Contains(imported, "\n") {
		t.Errorf("the imported configuration is not pretty-printed: %q", imported)
	}
	if _, err := h.svc.Compare(imported, configJSON, "imported", "original"); err != nil {
		t.Fatalf("the imported configuration does not compare: %v", err)
	}

	// Detection is read-only: an import prompt must not rewrite the prototype.
	if after := statAll(t, dir); after != before {
		t.Errorf("detection changed the legacy files:\nbefore %s\nafter  %s", before, after)
	}

	// An empty directory is only guaranteed to report nothing when the machine
	// has no ~/.singboxui from the prototype installed.
	if home, err := os.UserHomeDir(); err == nil {
		if _, statErr := os.Stat(filepath.Join(home, ".singboxui", "config.json")); statErr != nil {
			empty := h.svc.DetectLegacy(t.TempDir())
			if empty.Found {
				t.Errorf("an empty directory detected a legacy configuration at %q", empty.ConfigPath)
			}
		}
	}

	if _, err := h.svc.LegacyConfigJSON(LegacyCandidate{}); err == nil {
		t.Error("importing from an undetected candidate succeeded, want an error")
	}
	if _, err := h.svc.LegacyConfigJSON(LegacyCandidate{Found: true, ConfigPath: filepath.Join(dir, "missing.json")}); err == nil {
		t.Error("importing a missing configuration succeeded, want an error")
	}
}

func TestDetectLegacyRejectsAPrototypeConfigThatIsNotValidSingBoxJSON(t *testing.T) {
	h := newHarness(t)
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "config.json"), `{"outbounds":[]}`)

	candidate := h.svc.DetectLegacy(dir)
	if !candidate.Found {
		t.Fatal("a config.json was not detected")
	}
	if candidate.Valid {
		t.Error("an empty outbound list was reported as valid")
	}
	if len(candidate.Warnings) == 0 {
		t.Error("no warning was produced for the invalid prototype configuration")
	}
	if !hasWarning(candidate.Warnings, "outbounds") {
		t.Errorf("warnings = %v, want one about the outbounds", candidate.Warnings)
	}
}

func hasWarning(warnings []string, needle string) bool {
	for _, warning := range warnings {
		if strings.Contains(warning, needle) {
			return true
		}
	}
	return false
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("writing %s: %v", path, err)
	}
}

// statAll snapshots the files under dir so a test can prove nothing was written.
func statAll(t *testing.T, dir string) string {
	t.Helper()
	var builder strings.Builder
	err := filepath.WalkDir(dir, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(dir, path)
		if err != nil {
			return err
		}
		builder.WriteString(rel)
		if !entry.IsDir() {
			info, err := entry.Info()
			if err != nil {
				return err
			}
			builder.WriteString(":" + info.ModTime().UTC().String())
		}
		builder.WriteString("\n")
		return nil
	})
	if err != nil {
		t.Fatalf("walking %s: %v", dir, err)
	}
	return builder.String()
}
