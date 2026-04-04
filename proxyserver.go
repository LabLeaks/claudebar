package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/lableaks/claudebar/openrouter"
)

// runProxyServer runs the OpenRouter proxy as a long-lived foreground process.
// Called via: claudebar _proxy_server
// Spawned as a detached background process by ensureProxyRunning.
// Reads preset configs from stdin as JSON on startup.
func runProxyServer() {
	proxy, err := openrouter.NewProxy()
	if err != nil {
		fmt.Fprintf(os.Stderr, "[proxy] error: %v\n", err)
		os.Exit(1)
	}

	// Read initial presets from stdin (JSON map of name -> ProxyConfig)
	var presets map[string]openrouter.ProxyConfig
	if err := json.NewDecoder(os.Stdin).Decode(&presets); err != nil {
		fmt.Fprintf(os.Stderr, "[proxy] error reading presets: %v\n", err)
		os.Exit(1)
	}
	for name, cfg := range presets {
		proxy.RegisterPreset(name, cfg)
	}

	// Start server
	go func() {
		if err := proxy.Start(openrouter.DefaultPort); err != nil && err != http.ErrServerClosed {
			fmt.Fprintf(os.Stderr, "[proxy] server error: %v\n", err)
			os.Exit(1)
		}
	}()

	// Wait for signal
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGTERM, syscall.SIGINT)
	<-sigChan

	proxy.Stop()
}
