package config

import (
	"strings"

	domainconfig "github.com/larffxx/singboxui/internal/domain/config"
)

// ClashAPI is the local control endpoint of a configuration. Traffic monitoring
// needs it; it is never exposed outside the application (spec §30).
type ClashAPI struct {
	Enabled bool   `json:"enabled"`
	BaseURL string `json:"baseUrl"`
	Secret  string `json:"-"`
}

// defaultClashController is sing-box's own default when the field is omitted.
const defaultClashController = "127.0.0.1:9090"

// ClashAPIFromConfig extracts the Clash API settings from a raw configuration.
// It is deliberately tolerant: a configuration without a control API is valid
// and simply disables traffic monitoring.
func ClashAPIFromConfig(raw []byte) ClashAPI {
	root, err := domainconfig.Parse(raw)
	if err != nil {
		return ClashAPI{}
	}
	experimental, ok := root["experimental"].(map[string]any)
	if !ok {
		return ClashAPI{}
	}
	clash, ok := experimental["clash_api"].(map[string]any)
	if !ok {
		return ClashAPI{}
	}
	controller := strings.TrimSpace(stringValue(clash["external_controller"]))
	if controller == "" {
		controller = defaultClashController
	}
	return ClashAPI{
		Enabled: true,
		BaseURL: "http://" + controller,
		Secret:  strings.TrimSpace(stringValue(clash["secret"])),
	}
}

// SetClashAPIEnabled patches the control API into a configuration, preserving
// unknown fields (spec §36).
func SetClashAPIEnabled(raw []byte, enabled bool, controller, secret string) ([]byte, error) {
	root, err := domainconfig.Parse(raw)
	if err != nil {
		return nil, err
	}
	experimental, ok := root["experimental"].(map[string]any)
	if !ok {
		experimental = map[string]any{}
		root["experimental"] = experimental
	}
	if !enabled {
		delete(experimental, "clash_api")
		if len(experimental) == 0 {
			delete(root, "experimental")
		}
		return domainconfig.Marshal(root)
	}
	if strings.TrimSpace(controller) == "" {
		controller = defaultClashController
	}
	clash, ok := experimental["clash_api"].(map[string]any)
	if !ok {
		clash = map[string]any{}
		experimental["clash_api"] = clash
	}
	clash["external_controller"] = controller
	if secret != "" {
		clash["secret"] = secret
	}
	return domainconfig.Marshal(root)
}

func stringValue(value any) string {
	text, _ := value.(string)
	return text
}
