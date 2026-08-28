package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
)

const (
	probePrompt = "Reply with exactly HUMANSH_READY and nothing else. Do not use tools or inspect external state."
	e2eRequest  = "list all the files in this directory"
)

func main() {
	args := os.Args[1:]
	if len(args) == 2 && args[0] == "exec" && args[1] == probePrompt {
		fmt.Println("HUMANSH_READY")
		return
	}
	if len(args) == 0 || args[0] != "exec" {
		fail("unexpected Codex fixture invocation")
	}

	outputPath := ""
	for index := 0; index+1 < len(args); index++ {
		if args[index] == "--output-last-message" {
			outputPath = args[index+1]
			break
		}
	}
	if outputPath == "" || args[len(args)-1] != "-" {
		fail("translation invocation omitted its stdin or result channel")
	}
	input, err := io.ReadAll(io.LimitReader(os.Stdin, 1<<20))
	if err != nil {
		fail("read translation request: %v", err)
	}
	if !bytes.Contains(input, []byte(e2eRequest)) {
		fail("translation fixture received an unexpected request")
	}

	response := struct {
		Status        string   `json:"status"`
		Command       string   `json:"command"`
		Explanation   string   `json:"explanation"`
		Clarification string   `json:"clarification"`
		Assumptions   []string `json:"assumptions"`
	}{
		Status:      "ok",
		Command:     "ls -la",
		Explanation: "Lists every entry in the current directory.",
		Assumptions: []string{},
	}
	payload, err := json.Marshal(response)
	if err != nil {
		fail("encode translation response: %v", err)
	}
	if err := os.WriteFile(outputPath, payload, 0o600); err != nil {
		fail("write translation response: %v", err)
	}
}

func fail(format string, args ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", args...)
	os.Exit(2)
}
