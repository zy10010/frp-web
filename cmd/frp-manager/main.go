package main

import (
	"flag"
	"fmt"
	"os"
)

func main() {
	cfgPath := flag.String("c", "./frp-manager.toml", "path to the manager config file")
	showVersion := flag.Bool("version", false, "print version and exit")
	flag.Parse()

	if *showVersion {
		fmt.Println(managerVersion)
		return
	}

	m, err := NewManager(*cfgPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "frp-manager: %v\n", err)
		os.Exit(1)
	}
	m.Run()
}

// managerVersion is the version of frp-manager itself. It can be overridden at
// build time via -ldflags "-X main.managerVersion=...".
var managerVersion = "1.0.0"
