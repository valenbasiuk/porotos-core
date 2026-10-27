//go:build !windows

package main

import (
	"fmt"
	"os"
)

// El launcher es solo para Windows; esto permite compilar y testear la lógica en Linux.
func main() {
	fmt.Fprintln(os.Stderr, "Porotos 2 Launcher solo funciona en Windows.")
	os.Exit(1)
}
