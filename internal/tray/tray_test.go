package tray

import (
	"encoding/json"
	"testing"
)

// TestModelJSONIsThePlatformContract pins the wire shape the platform drivers
// receive. The Cocoa driver decodes exactly these names, so a rename here is a
// menu bar that silently stops updating.
func TestModelJSONIsThePlatformContract(t *testing.T) {
	t.Parallel()

	model := Model{
		Icon:    IconActive,
		Tooltip: "SingBoxUI — Запущен",
		Items: []Item{
			{Title: "Запущен", Enabled: false},
			{Separator: true},
			{ID: "show-window", Title: "Показать окно", Enabled: true},
		},
	}
	encoded, err := json.Marshal(model)
	if err != nil {
		t.Fatalf("json.Marshal(Model) failed: %v", err)
	}
	want := `{"icon":"active","tooltip":"SingBoxUI — Запущен","items":[` +
		`{"title":"Запущен","enabled":false},` +
		`{"enabled":false,"separator":true},` +
		`{"id":"show-window","title":"Показать окно","enabled":true}]}`
	if string(encoded) != want {
		t.Errorf("model JSON = %s\nwant %s", encoded, want)
	}
}

// TestIconNamesAreStable keeps the platform-neutral names the drivers map onto
// their own drawing.
func TestIconNamesAreStable(t *testing.T) {
	t.Parallel()

	cases := []struct {
		got  Icon
		want string
	}{
		{got: IconIdle, want: "idle"},
		{got: IconActive, want: "active"},
		{got: IconBusy, want: "busy"},
		{got: IconFailed, want: "failed"},
	}
	for _, tc := range cases {
		if string(tc.got) != tc.want {
			t.Errorf("icon = %q, want %q", tc.got, tc.want)
		}
	}
}
