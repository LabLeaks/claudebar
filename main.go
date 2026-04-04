package main

import (
	"fmt"
	"os"
)

// Set by goreleaser ldflags
var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

func main() {
	if len(os.Args) < 2 {
		runDefault()
		return
	}

	switch os.Args[1] {
	// User-facing
	case "help", "--help", "-h":
		runHelp()
	case "version", "--version", "-v":
		fmt.Printf("claudebar %s (%s, %s)\n", version, commit, date)
	case "sessions", "s":
		runSessions()
	case "--resumable":
		runResumable()

	// Internal (called by tmux keybinds)
	case "_status":
		runStatus()
	case "_detach":
		runDetach()
	case "_upgrade":
		runUpgrade()
	case "_perms":
		runPerms()
	case "_rc":
		runToggleRC()
	case "_tasks":
		runTasks()
	case "_shell":
		runShell()
	case "_help":
		runHelpPopup()
	case "_menu":
		runMenu()
	case "_send":
		// Send a slash command to claude: claudebar _send /compact
		if len(os.Args) > 2 {
			runSendToPane(os.Args[2])
		}
	case "_kill":
		runKillSession()
	case "_taskview":
		runTaskViewer()
	case "_agentview":
		runAgentViewer()
	case "_agents":
		runAgents()
	case "_compact":
		runSendToPane("/compact")
	case "_clear":
		runSendToPane("/clear")
	case "_verbose":
		runSendToPane("/verbose")
	case "_usage":
		runSendToPane("/usage")
	case "_features":
		runFeatures()
	case "_toggle":
		if len(os.Args) > 2 {
			runToggleFeature(os.Args[2])
		}
	case "_apply":
		runApply()
	case "_router":
		runRouterMenu()
	case "_toggle_router":
		if len(os.Args) > 2 {
			runToggleRouter(os.Args[2])
		}
	case "_new_router":
		runRouterWizard()
	case "_statusline":
		runStatusLine()
	case "_proxy_server":
		runProxyServer()

	default:
		// Anything else: pass through as claude flags
		runDefault()
	}
}
