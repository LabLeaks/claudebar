package main

import (
	"fmt"
	"os"
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
		fmt.Println("claudebar v0.1.0")

	// Internal (called by tmux keybinds)
	case "_status":
		runStatus()
	case "_detach":
		runDetach()
	case "_upgrade":
		runUpgrade()
	case "_perms":
		runPerms()
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
	case "_cost":
		runSendToPane("/cost")
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
	case "_cleanup":
		runCleanup()
	case "_check-main":
		runCheckMain()
	case "_statusline":
		runStatusLine()

	default:
		// Anything else: pass through as claude flags
		runDefault()
	}
}
