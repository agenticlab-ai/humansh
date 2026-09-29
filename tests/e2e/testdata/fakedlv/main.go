package main

import (
	"fmt"
	"os"
	"strings"
)

func main() {
	const multipleAssignmentsArgs = "dap\x00--log=true\x00--log-output=debugger\x00--client-addr=:59908"
	const singleAssignmentArgs = "dap\x00--log=true\x00--log-output=debugger\x00--client-addr=:55726"
	args := strings.Join(os.Args[1:], "\x00")
	backend := os.Getenv("PYXIS_AGENT_BACKEND")
	validInvocation := args == multipleAssignmentsArgs && backend == "cursor" || args == singleAssignmentArgs && backend == ""
	if !validInvocation || os.Getenv("GOPATH") == "" {
		fmt.Fprintf(os.Stderr, "HUMANSH_E2E_UNEXPECTED_EXECUTION: args=%q backend=%q\n", os.Args[1:], os.Getenv("PYXIS_AGENT_BACKEND"))
		os.Exit(97)
	}
	if _, err := os.Stat(os.Getenv("GOPATH") + "/bin/dlv"); err != nil {
		fmt.Fprintf(os.Stderr, "HUMANSH_E2E_UNEXPECTED_EXECUTION: GOPATH=%q: %v\n", os.Getenv("GOPATH"), err)
		os.Exit(97)
	}
	fmt.Println("HUMANSH_E2E_DLV_EXECUTED")
}
