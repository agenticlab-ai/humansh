package main

import (
	"fmt"
	"io"
	"os"
	"strings"
	"time"
)

func main() {
	if len(os.Args) < 2 {
		os.Exit(70)
	}
	mode := os.Args[1]
	inputBytes, _ := io.ReadAll(io.LimitReader(os.Stdin, 1<<20))
	input := string(inputBytes)
	if path := os.Getenv("HUMANSH_FAKE_CALLS"); path != "" {
		if file, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600); err == nil {
			_, _ = fmt.Fprintf(file, "%s|%s|argv=%q\n", mode, strings.ReplaceAll(input, "\n", `\n`), os.Args[2:])
			_ = file.Close()
		}
	}
	switch mode {
	case "translate":
		switch input {
		case "show me files":
			fmt.Print("printf '%s\\n' BASH_GENERATED_LOW")
			os.Exit(10)
		case "slow please":
			time.Sleep(2 * time.Second)
			fmt.Print("printf '%s\\n' BASH_SLOW")
			os.Exit(10)
		case "delete build":
			fmt.Print("printf '%s\\n' BASH_GENERATED_HIGH")
			os.Exit(14)
		default:
			fmt.Fprint(os.Stderr, "unexpected fake Bash translation input")
			os.Exit(70)
		}
	case "analyze":
		if strings.Contains(input, "HIGH") {
			os.Exit(14)
		}
		os.Exit(10)
	default:
		os.Exit(70)
	}
}
