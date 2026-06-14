package main

import (
	"fmt"
	"os"
)

// root.go — backend-dashboard entry point (operator REST API process).
// Does not start sniper workers, orchestrator, Telegram, or wallet/RPC clients.

func main() {
	if len(os.Args) < 2 {
		fmt.Println("Usage: dashboard-api <command>")
		fmt.Println("Commands: serve")
		os.Exit(1)
	}

	switch os.Args[1] {
	case "serve":
		runServe()
	default:
		fmt.Fprintf(os.Stderr, "Unknown command: %s\n", os.Args[1])
		os.Exit(1)
	}
}
