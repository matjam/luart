// Command gen writes vm_jit.go from vm.go in the current directory. See
// package jitvm.
package main

import (
	"log"
	"os"

	"github.com/matjam/apogee/internal/jitvm"
)

func main() {
	src, err := os.ReadFile("vm.go")
	if err != nil {
		log.Fatal(err)
	}
	out, err := jitvm.Generate(src)
	if err != nil {
		log.Fatal(err)
	}
	if err := os.WriteFile("vm_jit.go", out, 0o644); err != nil {
		log.Fatal(err)
	}
}
