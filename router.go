package main

import (
	"encoding/json"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/lableaks/claudebar/openrouter"
)

const (
	proxyDialTimeout     = 500 * time.Millisecond
	proxyRetryDelay      = 200 * time.Millisecond
	legacyKillGracePeriod = 500 * time.Millisecond
)

// routerConfig defines a named router configuration in claudebar's config.json.
type routerConfig struct {
	Provider  string            `json:"provider"`
	APIKey    string            `json:"api_key"`
	Models    map[string]string `json:"models"`
	Context1M bool              `json:"context_1m,omitempty"`
}

// knownProviders maps provider short names to their API base URLs.
var knownProviders = map[string]string{
	"openrouter": "https://openrouter.ai/api/v1/chat/completions",
}

// routerEnvVars returns the env vars to inject into a tmux session when routing.
// Returns nil when routerName is empty (no router active).
func routerEnvVars(routerName string, rc *routerConfig, tmuxSession string) []string {
	if routerName == "" {
		return nil
	}
	if rc == nil {
		return nil
	}

	baseURL := fmt.Sprintf("http://127.0.0.1:%d/preset/%s/v1/messages", openrouter.DefaultPort, routerName)
	if tmuxSession != "" {
		baseURL += "?session=" + tmuxSession
	}
	envs := []string{
		fmt.Sprintf("ANTHROPIC_BASE_URL=%s", baseURL),
		"ANTHROPIC_AUTH_TOKEN=claudebar",
		"ANTHROPIC_API_KEY=",
		"DISABLE_PROMPT_CACHING=1",
		"DISABLE_COST_WARNINGS=1",
		"NO_PROXY=127.0.0.1",
		"ENABLE_TOOL_SEARCH=true",
	}
	if rc.Context1M {
		envs = append(envs,
			"ANTHROPIC_DEFAULT_OPUS_MODEL=claude-opus-4-6[1m]",
			"ANTHROPIC_DEFAULT_SONNET_MODEL=claude-sonnet-4-6[1m]",
		)
	}
	return envs
}

// lookupRouterConfig loads the global config and returns the named router config, or nil.
func lookupRouterConfig(name string) *routerConfig {
	if name == "" {
		return nil
	}
	cfg := loadConfig()
	if cfg.RouterConfigs == nil {
		return nil
	}
	return cfg.RouterConfigs[name]
}

// extractRouterFlag parses --router=<name> or --router <name> from args.
// Returns the router name and the remaining args with the flag removed.
func extractRouterFlag(args []string) (string, []string) {
	var router string
	var remaining []string
	for i := 0; i < len(args); i++ {
		if strings.HasPrefix(args[i], "--router=") {
			router = strings.TrimPrefix(args[i], "--router=")
		} else if args[i] == "--router" && i+1 < len(args) {
			router = args[i+1]
			i++ // skip next arg
		} else {
			remaining = append(remaining, args[i])
		}
	}
	return router, remaining
}

// validateRouterConfig checks that a router config has all required fields.
func validateRouterConfig(name string, rc *routerConfig) error {
	if rc.Provider == "" {
		return fmt.Errorf("router config %q: missing required field 'provider'", name)
	}
	if _, ok := knownProviders[rc.Provider]; !ok {
		known := make([]string, 0, len(knownProviders))
		for k := range knownProviders {
			known = append(known, k)
		}
		return fmt.Errorf("router config %q: unknown provider %q (known: %s)", name, rc.Provider, strings.Join(known, ", "))
	}
	if rc.APIKey == "" {
		return fmt.Errorf("router config %q: missing required field 'api_key'", name)
	}
	if strings.HasPrefix(rc.APIKey, "$") {
		envName := rc.APIKey[1:]
		if os.Getenv(envName) == "" {
			return fmt.Errorf("router config %q: api_key references $%s but it is not set", name, envName)
		}
	}
	if len(rc.Models) == 0 {
		return fmt.Errorf("router config %q: missing required field 'models' (at least 'default' slot required)", name)
	}
	if _, ok := rc.Models["default"]; !ok {
		return fmt.Errorf("router config %q: models must include 'default' slot", name)
	}
	return nil
}

// countRoutedSessions counts live tmux sessions that have an active router.
func countRoutedSessions() int {
	live := liveTmuxSessionsFunc()
	count := 0
	entries, err := os.ReadDir(stateDir())
	if err != nil {
		return 0
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".state.json") {
			continue
		}
		name := strings.TrimSuffix(e.Name(), ".state.json")
		if !live[name] {
			continue
		}
		data, err := os.ReadFile(filepath.Join(stateDir(), e.Name()))
		if err != nil {
			continue
		}
		var s claudeSessionState
		if err := json.Unmarshal(data, &s); err != nil {
			continue
		}
		if s.Router != "" {
			count++
		}
	}
	return count
}

// runRouterMenu shows the top-level router submenu.
func runRouterMenu() {
	sess := currentSession()
	state := loadState(sess)
	cfg := loadConfig()
	self := selfPath()

	args := routerMenuItems(self, sess, state, cfg)

	// Show "Apply & restart" when toggles have been changed
	if state.PendingRestart {
		args = append(args, "")
		args = append(args,
			"#[fg=#ff8800,bold]  Apply & restart ↵", "",
			fmt.Sprintf("run-shell '%s _apply'", self),
		)
	}

	tmuxExec(append([]string{"display-menu", "-T", " #[fg=#00d4ff,bold]Router  " + featureLegend()}, args...)...)
}

func featureLegend() string {
	return "○ off  ● on  ◉ always "
}

// routerMenuItems returns tmux display-menu args for the router submenu.
func routerMenuItems(self, sess string, state *claudeSessionState, cfg *globalConfig) []string {
	var args []string

	if len(cfg.RouterConfigs) > 0 {
		names := make([]string, 0, len(cfg.RouterConfigs))
		for name := range cfg.RouterConfigs {
			names = append(names, name)
		}
		sort.Strings(names)

		for i, name := range names {
			sessionOn := state.Router == name
			configOn := cfg.Router == name
			label := fmt.Sprintf("%s  %s", featureState(sessionOn, configOn), name)
			key := strconv.Itoa(i + 1)
			args = append(args, label, key,
				fmt.Sprintf("run-shell '%s _toggle_router %s'", self, name))
		}
	}

	args = append(args, "#[fg=#00ff88]  + New router config...", "n",
		fmt.Sprintf("display-popup -E -w 60 -h 20 '%s _new_router'", self))

	return args
}

// runToggleRouter implements the radio-button cycle for router configs.
// OFF → ON → ALWAYS → OFF. Only one router active at a time.
func runToggleRouter(configName string) {
	sess := currentSession()
	state := loadState(sess)
	if state.WorkDir == "" {
		dir, _ := os.Getwd()
		state.WorkDir = dir
	}
	cfg := loadConfig()

	// Validate config exists and is valid
	rc, ok := cfg.RouterConfigs[configName]
	if !ok {
		tmuxExec("display-message", fmt.Sprintf("Router config %q not found", configName))
		return
	}
	if err := validateRouterConfig(configName, rc); err != nil {
		tmuxExec("display-message", fmt.Sprintf("Invalid config: %v", err))
		return
	}

	// Current state for this config
	sessionOn := state.Router == configName
	configOn := cfg.Router == configName

	newSession, newConfig := cycleFeature(sessionOn, configOn)

	if newSession {
		state.Router = configName
	} else {
		state.Router = ""
	}
	if newConfig {
		cfg.Router = configName
	} else {
		if cfg.Router == configName {
			cfg.Router = ""
		}
	}

	saveConfig(cfg)

	newState := featureState(state.Router == configName, cfg.Router == configName)

	// Check if session state actually changed
	old := loadState(sess)
	sessionChanged := old.Router != state.Router

	if sessionChanged {
		state.PendingRestart = true
	}

	saveState(sess, state)
	tmuxExec("display-message", fmt.Sprintf("%s → %s", configName, newState))

	// Re-show the menu so user can make more changes
	runRouterMenu()
}

// cleanupOrphanedTransports stops the proxy if no sessions use it.
func cleanupOrphanedTransports() {
	// Kill any leftover CCR processes from before the cutover
	killLegacyCCR()

	if _, alive := proxyRunning(); alive && countRoutedSessions() == 0 {
		stopProxy()
	}
}

// killLegacyCCR kills any running CCR process and cleans up its files.
// Called once on startup to clean up from the pre-cutover era.
func killLegacyCCR() {
	pidFile := filepath.Join(configDir(), "ccr.pid")
	data, err := os.ReadFile(pidFile)
	if err != nil {
		return
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil || pid <= 0 {
		os.Remove(pidFile)
		return
	}
	proc, err := os.FindProcess(pid)
	if err != nil {
		os.Remove(pidFile)
		return
	}
	// Kill it if it's still alive
	if err := proc.Signal(syscall.Signal(0)); err == nil {
		proc.Signal(syscall.SIGTERM)
		time.Sleep(legacyKillGracePeriod)
		proc.Kill()
	}
	os.Remove(pidFile)
}

// -- Native proxy lifecycle --

func proxyPidFile() string {
	return filepath.Join(configDir(), "openrouter-proxy.pid")
}

// proxyRunning checks if the native proxy process is alive.
func proxyRunning() (int, bool) {
	data, err := os.ReadFile(proxyPidFile())
	if err != nil {
		return 0, false
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil || pid <= 0 {
		return 0, false
	}
	proc, err := os.FindProcess(pid)
	if err != nil {
		return 0, false
	}
	if err := proc.Signal(syscall.Signal(0)); err != nil {
		os.Remove(proxyPidFile())
		return pid, false
	}
	return pid, true
}

// ensureProxyRunning starts the native proxy as a detached background process if needed.
// The proxy runs as `claudebar _proxy_server` with preset configs piped via stdin.
func ensureProxyRunning(cfg *globalConfig, routerName string) error {
	rc := cfg.RouterConfigs[routerName]
	if rc == nil {
		return fmt.Errorf("router config %q not found", routerName)
	}

	// If proxy is already running, just check the port is live
	if _, alive := proxyRunning(); alive {
		conn, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", openrouter.DefaultPort), proxyDialTimeout)
		if err == nil {
			conn.Close()
			return nil
		}
		// PID alive but port dead — stale process, kill it
		stopProxy()
	}

	// Build preset configs to send via stdin
	presets := make(map[string]openrouter.ProxyConfig)
	for name, rc := range cfg.RouterConfigs {
		apiKey := rc.APIKey
		if strings.HasPrefix(apiKey, "$") {
			apiKey = os.Getenv(apiKey[1:])
			if apiKey == "" {
				continue // skip configs with unresolvable keys
			}
		}
		presets[name] = openrouter.ProxyConfig{
			APIKey:  apiKey,
			BaseURL: knownProviders[rc.Provider],
			Models:  openrouter.ParseModelSlots(rc.Models),
		}
	}

	presetsJSON, err := json.Marshal(presets)
	if err != nil {
		return fmt.Errorf("marshaling presets: %w", err)
	}

	// Spawn `claudebar _proxy_server` as detached background process
	self := selfPath()
	cmd := exec.Command(self, "_proxy_server")
	cmd.Stdin = strings.NewReader(string(presetsJSON))
	cmd.Stdout = nil
	// Log proxy stderr for debugging panics and errors
	logFile, _ := os.OpenFile(filepath.Join(configDir(), "proxy.log"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
	cmd.Stderr = logFile
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("starting proxy: %w", err)
	}

	// Write PID file
	pid := cmd.Process.Pid
	if err := os.WriteFile(proxyPidFile(), []byte(strconv.Itoa(pid)), 0600); err != nil {
		cmd.Process.Kill()
		return fmt.Errorf("writing proxy PID file: %w", err)
	}

	// Poll for port liveness (up to 5 seconds)
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		conn, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", openrouter.DefaultPort), proxyDialTimeout)
		if err == nil {
			conn.Close()
			return nil
		}
		time.Sleep(proxyRetryDelay)
	}

	return fmt.Errorf("proxy started (pid %d) but port %d not ready after 5s", pid, openrouter.DefaultPort)
}

// stopProxy kills the native proxy process.
func stopProxy() {
	pid, alive := proxyRunning()
	if !alive {
		os.Remove(proxyPidFile())
		return
	}
	proc, err := os.FindProcess(pid)
	if err == nil {
		proc.Signal(syscall.SIGTERM)
		time.Sleep(500 * time.Millisecond)
		if _, still := proxyRunning(); still {
			proc.Kill()
		}
	}
	os.Remove(proxyPidFile())
}
