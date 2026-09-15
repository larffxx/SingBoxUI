package settings

import (
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"testing"
)

func TestValidateAcceptsKnownValues(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input Settings
	}{
		{name: "fresh defaults", input: Default()},
		{
			name: "managed binary with a stale custom path",
			input: Settings{
				BinarySource:     BinaryManaged,
				CustomBinaryPath: "/usr/local/bin/sing-box",
				Theme:            ThemeDark,
				LogLevel:         LogWarn,
			},
		},
		{
			name: "custom binary with an absolute path",
			input: Settings{
				// Validate only requires a non-empty path; it is the resolver
				// (singbox.Locate) that insists on absoluteness, so a relative
				// path is deliberately accepted here.
				BinarySource:     BinaryCustom,
				CustomBinaryPath: "/opt/sing-box",
				Theme:            ThemeSystem,
				LogLevel:         LogDebug,
			},
		},
		{name: "light theme and error level", input: Settings{BinarySource: BinaryManaged, Theme: ThemeLight, LogLevel: LogError}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if err := tc.input.Validate(); err != nil {
				t.Fatalf("Validate() = %v, want nil", err)
			}
		})
	}
}

func TestValidateRejectsUnknownEnumsAndMissingPaths(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		input     Settings
		wantField string
	}{
		{
			name:      "unknown binary source",
			input:     Settings{BinarySource: "system", Theme: ThemeSystem, LogLevel: LogInfo},
			wantField: "binarySource",
		},
		{
			name:      "empty binary source",
			input:     Settings{Theme: ThemeSystem, LogLevel: LogInfo},
			wantField: "binarySource",
		},
		{
			name:      "custom source without a path",
			input:     Settings{BinarySource: BinaryCustom, CustomBinaryPath: "   ", Theme: ThemeSystem, LogLevel: LogInfo},
			wantField: "customBinaryPath",
		},
		{
			name:      "unknown theme",
			input:     Settings{BinarySource: BinaryManaged, Theme: "neon", LogLevel: LogInfo},
			wantField: "theme",
		},
		{
			name:      "unknown log level",
			input:     Settings{BinarySource: BinaryManaged, Theme: ThemeDark, LogLevel: "trace"},
			wantField: "logLevel",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			err := tc.input.Validate()
			if err == nil {
				t.Fatal("Validate() = nil, want an error")
			}
			var invalid *ErrInvalid
			if !errors.As(err, &invalid) {
				t.Fatalf("Validate() = %T, want *ErrInvalid", err)
			}
			if invalid.Field != tc.wantField {
				t.Errorf("Field = %q, want %q", invalid.Field, tc.wantField)
			}
			if !strings.HasPrefix(err.Error(), tc.wantField+": ") {
				t.Errorf("Error() = %q, want it to name the field", err.Error())
			}
		})
	}
}

func TestWithDefaultsFillsOnlyEmptyEnums(t *testing.T) {
	t.Parallel()

	def := Default()

	t.Run("empty enums take the defaults", func(t *testing.T) {
		t.Parallel()
		got := Settings{}.WithDefaults()
		if got.BinarySource != def.BinarySource {
			t.Errorf("BinarySource = %q, want %q", got.BinarySource, def.BinarySource)
		}
		if got.Theme != def.Theme {
			t.Errorf("Theme = %q, want %q", got.Theme, def.Theme)
		}
		if got.LogLevel != def.LogLevel {
			t.Errorf("LogLevel = %q, want %q", got.LogLevel, def.LogLevel)
		}
		// A settings row written by an older build must load, so the result of
		// filling the defaults has to validate.
		if err := got.Validate(); err != nil {
			t.Errorf("Validate() after WithDefaults() = %v, want nil", err)
		}
	})

	t.Run("explicit values are kept", func(t *testing.T) {
		t.Parallel()
		in := Settings{BinarySource: BinaryCustom, CustomBinaryPath: "/opt/sing-box", Theme: ThemeDark, LogLevel: LogError}
		got := in.WithDefaults()
		if got != in {
			t.Errorf("WithDefaults() = %+v, want the input %+v unchanged", got, in)
		}
	})
}

func TestDefaultIsUsableAndSelfConsistent(t *testing.T) {
	t.Parallel()

	def := Default()
	if err := def.Validate(); err != nil {
		t.Fatalf("Default().Validate() = %v, want nil", err)
	}
	if def.BinarySource != BinaryManaged {
		t.Errorf("BinarySource = %q, want managed so a fresh install uses the verified binary", def.BinarySource)
	}
	if !def.UpdateCheckEnabled {
		t.Error("UpdateCheckEnabled = false, want true on a fresh install")
	}
	if !def.ManagedStableChannel {
		t.Error("ManagedStableChannel = false, want true on a fresh install")
	}
	if def.AutoStartApplication || def.AutoConnect {
		t.Error("a fresh install must not silently opt the user into autostart or auto-connect")
	}
}

func TestMarshalUnmarshalRoundTripsEveryField(t *testing.T) {
	t.Parallel()

	in := Settings{
		AutoStartApplication: true,
		AutoConnect:          true,
		BinarySource:         BinaryCustom,
		CustomBinaryPath:     "/Applications/sing-box",
		ManagedStableChannel: false,
		UpdateCheckEnabled:   false,
		Theme:                ThemeLight,
		LogLevel:             LogWarn,
		LastProfileID:        "profile-42",
	}

	raw, err := Marshal(in)
	if err != nil {
		t.Fatalf("Marshal() = %v, want nil", err)
	}
	if !strings.Contains(raw, `"version":1`) {
		t.Errorf("encoded settings %s is missing the version envelope", raw)
	}
	out, err := Unmarshal(raw)
	if err != nil {
		t.Fatalf("Unmarshal() = %v, want nil", err)
	}
	if out != in {
		t.Errorf("round trip = %+v, want %+v", out, in)
	}
}

func TestUnmarshalIgnoresUnknownKeys(t *testing.T) {
	t.Parallel()

	// A row written by a future build (or by the Java prototype) carries fields
	// this build does not know. They must be dropped, not surfaced: spec §49
	// forbids an untyped map, so an unknown key can never reach the UI.
	raw := `{"version":1,"values":{"theme":"dark","logLevel":"warn","binarySource":"managed",` +
		`"legacyJavaOption":true,"lastProfileId":"p1","nested":{"a":1},"themeColor":"#fff"}}`

	got, err := Unmarshal(raw)
	if err != nil {
		t.Fatalf("Unmarshal() = %v, want nil", err)
	}
	if got.Theme != ThemeDark || got.LogLevel != LogWarn || got.BinarySource != BinaryManaged || got.LastProfileID != "p1" {
		t.Errorf("known fields were not decoded: %+v", got)
	}

	// Re-encoding must not reintroduce the unknown keys.
	out, err := Marshal(got)
	if err != nil {
		t.Fatalf("Marshal() = %v, want nil", err)
	}
	var decoded map[string]any
	if err := json.Unmarshal([]byte(out), &decoded); err != nil {
		t.Fatalf("decode re-encoded settings: %v", err)
	}
	values, ok := decoded["values"].(map[string]any)
	if !ok {
		t.Fatalf("re-encoded settings has no values object: %s", out)
	}
	for _, key := range []string{"legacyJavaOption", "nested", "themeColor"} {
		if _, present := values[key]; present {
			t.Errorf("unknown key %q survived the round trip: %s", key, out)
		}
	}
	if len(values) != 9 {
		t.Errorf("encoded settings carry %d fields, want the 9 typed fields: %s", len(values), out)
	}
}

func TestUnmarshalToleratesEmptyAndMissingValues(t *testing.T) {
	t.Parallel()

	for _, raw := range []string{"", "   \n"} {
		t.Run("empty storage value "+strconv.Quote(raw), func(t *testing.T) {
			t.Parallel()
			got, err := Unmarshal(raw)
			if err != nil {
				t.Fatalf("Unmarshal(%q) = %v, want nil", raw, err)
			}
			if got != Default() {
				t.Errorf("Unmarshal(%q) = %+v, want Default() %+v", raw, got, Default())
			}
		})
	}

	// A stored envelope with no payload decodes to the zero Settings with only
	// the enum fields filled in: WithDefaults covers enums, not booleans, so
	// the two feature switches come back false rather than at their Default()
	// value. That is the current contract and it is asserted here so a change
	// in either direction is visible.
	for _, raw := range []string{`{"version":1}`, `{"version":1,"values":null}`} {
		t.Run("envelope without payload "+strconv.Quote(raw), func(t *testing.T) {
			t.Parallel()
			got, err := Unmarshal(raw)
			if err != nil {
				t.Fatalf("Unmarshal(%q) = %v, want nil", raw, err)
			}
			want := Settings{BinarySource: BinaryManaged, Theme: ThemeSystem, LogLevel: LogInfo}
			if got != want {
				t.Errorf("Unmarshal(%q) = %+v, want %+v", raw, got, want)
			}
			if got.ManagedStableChannel || got.UpdateCheckEnabled {
				t.Errorf("Unmarshal(%q) enabled a feature switch: %+v", raw, got)
			}
			if err := got.Validate(); err != nil {
				t.Errorf("Validate() on a decoded empty envelope = %v, want nil", err)
			}
		})
	}
}

func TestUnmarshalReportsMalformedJSON(t *testing.T) {
	t.Parallel()

	_, err := Unmarshal(`{"version":1,"values":`)
	if err == nil {
		t.Fatal("Unmarshal() = nil, want an error for truncated JSON")
	}
	if !strings.Contains(err.Error(), "decode settings") {
		t.Errorf("error %q should be recognisable as a decode failure", err)
	}
}

func TestEnumValidHelpers(t *testing.T) {
	t.Parallel()

	for _, source := range []BinarySource{BinaryManaged, BinaryCustom} {
		if !source.Valid() {
			t.Errorf("BinarySource(%q).Valid() = false, want true", source)
		}
	}
	if BinarySource("").Valid() || BinarySource("MANAGED").Valid() {
		t.Error("BinarySource.Valid() accepted an unknown value")
	}
	for _, theme := range []Theme{ThemeSystem, ThemeLight, ThemeDark} {
		if !theme.Valid() {
			t.Errorf("Theme(%q).Valid() = false, want true", theme)
		}
	}
	if Theme("blue").Valid() {
		t.Error("Theme.Valid() accepted an unknown value")
	}
	for _, level := range []LogLevel{LogDebug, LogInfo, LogWarn, LogError} {
		if !level.Valid() {
			t.Errorf("LogLevel(%q).Valid() = false, want true", level)
		}
	}
	if LogLevel("verbose").Valid() {
		t.Error("LogLevel.Valid() accepted an unknown value")
	}
}
