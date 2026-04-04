package openrouter

import (
	"os"
	"strings"
)

// ProxyConfig holds the OpenRouter proxy configuration
type ProxyConfig struct {
	APIKey  string
	BaseURL string
	Models  map[string]string // slot -> actual model mapping
}

// Resolve API key from env var reference (e.g. "$OPENROUTER_API_KEY" -> value of env var)
func resolveAPIKey(raw string) string {
	if strings.HasPrefix(raw, "$") {
		return os.Getenv(raw[1:])
	}
	return raw
}

// ParseModelSlots converts router config model slots to the proxy's model map.
// Input format: "slot" -> "provider,actual_model"
// Output format: "slot" -> "actual_model"
func ParseModelSlots(routerModels map[string]string) map[string]string {
	models := make(map[string]string)
	for slot, value := range routerModels {
		parts := strings.SplitN(value, ",", 2)
		if len(parts) == 2 {
			models[slot] = parts[1]
		} else {
			// No provider prefix — use value as-is
			models[slot] = value
		}
	}
	return models
}
