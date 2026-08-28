package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

var extraCommand string

func main() {
	executable, _ := os.Executable()
	name := filepath.Base(executable)
	stdin, _ := io.ReadAll(os.Stdin)
	logInvocation(executable, len(stdin))
	if strings.Contains(name, "hang") {
		time.Sleep(5 * time.Second)
	}
	if strings.Contains(name, "bsdusage") {
		fmt.Fprintln(os.Stderr, name+": illegal option -- -")
		fmt.Fprintf(os.Stderr, "usage: %s [-f | -i] [-dIPRrvWx] file ...\n", name)
		fmt.Fprintln(os.Stderr, "       unlink [--] file")
		os.Exit(64)
	}
	if strings.Contains(name, "huge") {
		fmt.Fprintln(os.Stdout, "Usage: fixturevcs <COMMAND>")
		fmt.Fprintln(os.Stdout, "Commands:")
		fmt.Fprintln(os.Stdout, "  status  show status")
		fmt.Fprint(os.Stdout, strings.Repeat("x", 1<<20))
		return
	}
	output := os.Stdout
	exitCode := 0
	if strings.Contains(name, "stderr") {
		output = os.Stderr
		exitCode = 7
	}

	args := strings.Join(os.Args[1:], " ")
	switch args {
	case "--help":
		commands := []string{"commit", "remote", "status"}
		if extraCommand != "" {
			commands = append(commands, extraCommand)
		}
		fmt.Fprintln(output, "Usage: fixturevcs [OPTIONS] <COMMAND> [ARGS]")
		fmt.Fprintln(output, "\nCommands:")
		for _, command := range commands {
			fmt.Fprintf(output, "  %-12s fixture command\n", command)
		}
		fmt.Fprintln(output, "\nOptions:")
		fmt.Fprintln(output, "  -C <DIR>             run from a directory")
		fmt.Fprintln(output, "      --config=<DIR>   configuration directory")
		fmt.Fprintln(output, "      --no-pager       disable paging")
		fmt.Fprintln(output, "  -h, --help           show help")
	case "status --help":
		fmt.Fprintln(output, "Usage: fixturevcs status [OPTIONS] [PATH]")
		fmt.Fprintln(output, "\nOptions:")
		fmt.Fprintln(output, "      --short                  short output")
		fmt.Fprintln(output, "      --porcelain[=VERSION]    stable output")
		fmt.Fprintln(output, "  -h, --help                   show help")
	case "commit --help":
		fmt.Fprintln(output, "Usage: fixturevcs commit [OPTIONS] [PATH]")
		fmt.Fprintln(output, "\nOptions:")
		fmt.Fprintln(output, "  -a                         stage tracked changes")
		fmt.Fprintln(output, "  -m <TEXT>, --message=<TEXT>  commit message")
		fmt.Fprintln(output, "  -h, --help                   show help")
	case "remote --help":
		fmt.Fprintln(output, "Usage: fixturevcs remote <COMMAND>")
		fmt.Fprintln(output, "\nCommands:")
		fmt.Fprintln(output, "  add          add a remote")
		fmt.Fprintln(output, "  show         show a remote")
	case "remote add --help", "remote show --help", "inspect --help":
		fmt.Fprintln(output, "Usage: fixturevcs operation [OPTIONS] [VALUE]")
		fmt.Fprintln(output, "\nOptions:")
		fmt.Fprintln(output, "  -h, --help  show help")
	default:
		fmt.Fprintln(os.Stderr, "unexpected fixture invocation:", args)
		os.Exit(97)
	}
	if exitCode != 0 {
		os.Exit(exitCode)
	}
}

func logInvocation(executable string, stdinBytes int) {
	file, err := os.OpenFile(executable+".log", os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0600)
	if err != nil {
		return
	}
	defer file.Close()
	cwd, _ := os.Getwd()
	_, _ = fmt.Fprintf(file, "args=%s stdin=%d cwd=%s home=%s locale=%s secret=%t\n", strings.Join(os.Args[1:], " "), stdinBytes, cwd, os.Getenv("HOME"), os.Getenv("LC_ALL"), os.Getenv("HUMANSH_SECRET_MARKER") != "")
}
