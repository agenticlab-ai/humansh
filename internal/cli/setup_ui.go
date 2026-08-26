package cli

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/agenticlab-ai/humansh/internal/config"
	"github.com/agenticlab-ai/humansh/internal/llm"
	"github.com/agenticlab-ai/humansh/internal/shell"
	"golang.org/x/term"
)

const (
	ansiReset  = "\x1b[0m"
	ansiBold   = "\x1b[1m"
	ansiDim    = "\x1b[2m"
	ansiCyan   = "\x1b[36m"
	ansiGreen  = "\x1b[32m"
	ansiRed    = "\x1b[31m"
	ansiYellow = "\x1b[33m"
)

type setupUI struct {
	streams     IO
	reader      *bufio.Reader
	ctx         context.Context
	interactive bool
	styled      bool
	animated    bool
}

func newSetupUI(streams IO, interactive bool) *setupUI {
	animated := writerIsTerminal(streams.Out) && os.Getenv("TERM") != "dumb"
	return &setupUI{
		streams:     streams,
		reader:      bufio.NewReader(streams.In),
		ctx:         context.Background(),
		interactive: interactive,
		styled:      interactive && animated && os.Getenv("NO_COLOR") == "",
		animated:    animated,
	}
}

type setupReadResult[T any] struct {
	value T
	err   error
}

func setupReadWithContext[T any](ctx context.Context, read func() (T, error)) (T, error) {
	var zero T
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	result := make(chan setupReadResult[T], 1)
	go func() {
		value, err := read()
		result <- setupReadResult[T]{value: value, err: err}
	}()
	select {
	case <-ctx.Done():
		return zero, ctx.Err()
	case value := <-result:
		return value.value, value.err
	}
}

func (ui *setupUI) readString(delimiter byte) (string, error) {
	return setupReadWithContext(ui.ctx, func() (string, error) {
		return ui.reader.ReadString(delimiter)
	})
}

func (ui *setupUI) readByte() (byte, error) {
	return setupReadWithContext(ui.ctx, ui.reader.ReadByte)
}

func writerIsTerminal(writer io.Writer) bool {
	file, ok := writer.(*os.File)
	return ok && term.IsTerminal(int(file.Fd()))
}

func (ui *setupUI) paint(style, text string) string {
	if !ui.styled {
		return text
	}
	return style + text + ansiReset
}

func (ui *setupUI) header() {
	fmt.Fprintln(ui.streams.Out)
	fmt.Fprintln(ui.streams.Out, ui.paint(ansiBold+ansiCyan, "humansh setup"))
	fmt.Fprintln(ui.streams.Out, ui.paint(ansiDim, "Natural-language commands for Zsh and Bash, with review before execution."))
	if ui.interactive {
		fmt.Fprintln(ui.streams.Out, ui.paint(ansiDim, "Credentials, configuration, and shell files are not changed until the final confirmation."))
	} else {
		fmt.Fprintln(ui.streams.Out, ui.paint(ansiDim, "Reviewing the effective configuration for this setup run."))
	}
}

func (ui *setupUI) section(number, total int, title string) {
	fmt.Fprintln(ui.streams.Out)
	label := fmt.Sprintf("%d/%d  %s", number, total, title)
	fmt.Fprintln(ui.streams.Out, ui.paint(ansiBold+ansiCyan, label))
}

func (ui *setupUI) withLoader(label string, work func()) {
	if !ui.animated {
		fmt.Fprintf(ui.streams.Out, "  … %s\n", label)
		work()
		return
	}

	frames := []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}
	writeFrame := func(frame string) {
		fmt.Fprintf(ui.streams.Out, "\r  %s %s", ui.paint(ansiCyan, frame), label)
	}
	writeFrame(frames[0])

	done := make(chan struct{})
	stopped := make(chan struct{})
	go func() {
		defer close(stopped)
		ticker := time.NewTicker(80 * time.Millisecond)
		defer ticker.Stop()
		frame := 1
		for {
			select {
			case <-done:
				return
			case <-ticker.C:
				writeFrame(frames[frame])
				frame = (frame + 1) % len(frames)
			}
		}
	}()

	work()
	close(done)
	<-stopped
	fmt.Fprintf(ui.streams.Out, "\r%s\r", strings.Repeat(" ", len([]rune(label))+6))
}

func (ui *setupUI) status(ok bool, label, detail string) {
	mark, style := "–", ansiDim
	if ok {
		mark, style = "✓", ansiGreen
	}
	fmt.Fprintf(ui.streams.Out, "  %s %-16s %s\n", ui.paint(style, mark), label, detail)
}

func (ui *setupUI) setting(label, current, defaultValue, detail string) {
	value := current
	if current == defaultValue {
		value += " " + ui.paint(ansiDim, "(default)")
	} else if defaultValue != "" {
		value += " " + ui.paint(ansiDim, "(default: "+defaultValue+")")
	}
	fmt.Fprintf(ui.streams.Out, "  %-22s %s\n", label, ui.paint(ansiBold, value))
	if detail != "" {
		fmt.Fprintf(ui.streams.Out, "    %s\n", ui.paint(ansiDim, detail))
	}
}

func (ui *setupUI) note(message string) {
	fmt.Fprintf(ui.streams.Out, "  %s %s\n", ui.paint(ansiCyan, "i"), message)
}

func (ui *setupUI) warning(message string) {
	fmt.Fprintf(ui.streams.Out, "  %s %s\n", ui.paint(ansiYellow, "!"), message)
}

func (ui *setupUI) success(message string) {
	fmt.Fprintf(ui.streams.Out, "  %s %s\n", ui.paint(ansiGreen, "✓"), message)
}

func (ui *setupUI) startupPatch(change config.StartupChange) {
	if !change.Changed() {
		ui.success("Shell activation: no changes needed in " + setupDisplayPath(change.Path) + ".")
		return
	}

	path := setupDisplayPath(change.Path)
	target := setupDisplayPath(change.TargetPath)
	fmt.Fprintln(ui.streams.Out)
	fmt.Fprintln(ui.streams.Out, "  "+ui.paint(ansiBold, "Shell activation patch"))
	if target != path {
		ui.note(path + " is a symlink; its target " + target + " will be updated.")
	}
	fmt.Fprintln(ui.streams.Out, "    "+ui.paint(ansiRed, "--- "+path+" (before)"))
	fmt.Fprintln(ui.streams.Out, "    "+ui.paint(ansiGreen, "+++ "+path+" (after)"))
	fmt.Fprintln(ui.streams.Out, "    "+ui.paint(ansiCyan, "@@ humansh managed block @@"))
	for _, line := range change.PatchLines() {
		style := ansiGreen
		if line.Kind == '-' {
			style = ansiRed
		}
		fmt.Fprintln(ui.streams.Out, "    "+ui.paint(style, string(line.Kind)+line.Text))
	}
	if change.RepositionsManagedBlock() {
		fmt.Fprintln(ui.streams.Out, "    "+ui.paint(ansiYellow, "~ reposition or normalize spacing around the existing block"))
	}
}

func setupDisplayPath(path string) string {
	home, err := os.UserHomeDir()
	if err != nil {
		return path
	}
	homes := []string{home}
	if resolved, resolveErr := filepath.EvalSymlinks(home); resolveErr == nil && resolved != home {
		homes = append(homes, resolved)
	}
	for _, candidate := range homes {
		if path == candidate {
			return "~"
		}
		prefix := candidate + string(os.PathSeparator)
		if strings.HasPrefix(path, prefix) {
			return "~/" + strings.TrimPrefix(path, prefix)
		}
	}
	return path
}

func (ui *setupUI) providerDiagnostic(id llm.ProviderID, diagnostic llm.Diagnostic) {
	if diagnostic.Available {
		ui.success("Using " + setupProviderName(id) + ".")
		return
	}
	ui.providerProblem(id, diagnostic)
	ui.providerRecovery(id, diagnostic)
}

func (ui *setupUI) providerProblem(id llm.ProviderID, diagnostic llm.Diagnostic) {
	ui.warning(setupProviderName(id) + ": " + setupProviderChoiceStatus(id, diagnostic) + ".")
	if diagnostic.Message != "" {
		ui.note(diagnostic.Message)
	}
	if diagnostic.Executable != "" {
		selected := "Executable " + strconv.Quote(diagnostic.Executable)
		if diagnostic.Version != "" {
			selected += " — " + diagnostic.Version
		}
		ui.note(selected)
	}
}

func (ui *setupUI) providerRecovery(id llm.ProviderID, diagnostic llm.Diagnostic) {
	actions := append([]llm.DiagnosticAction(nil), diagnostic.NextSteps...)
	if len(actions) == 0 {
		switch id {
		case llm.Codex:
			if diagnostic.Installed {
				actions = []llm.DiagnosticAction{{Description: "Check the provider-managed Codex setup", Command: "humansh provider test codex"}, {Description: "Rerun setup", Command: "humansh setup"}}
			} else {
				actions = []llm.DiagnosticAction{{Description: "Install Codex", Command: "curl -fsSL https://chatgpt.com/codex/install.sh | sh"}, {Description: "Rerun setup", Command: "humansh setup"}}
			}
		case llm.Claude:
			if diagnostic.Installed {
				actions = []llm.DiagnosticAction{{Description: "Check the provider-managed Claude Code setup", Command: "humansh provider test claude"}, {Description: "Rerun setup", Command: "humansh setup"}}
			} else {
				actions = []llm.DiagnosticAction{{Description: "Install Claude Code", Command: "curl -fsSL https://claude.ai/install.sh | bash"}, {Description: "Rerun setup", Command: "humansh setup"}}
			}
		case llm.Cursor:
			if diagnostic.Installed {
				actions = []llm.DiagnosticAction{{Description: "Check the provider-managed Cursor setup", Command: "humansh provider test cursor"}, {Description: "Rerun setup", Command: "humansh setup"}}
			} else {
				actions = []llm.DiagnosticAction{{Description: "Install Cursor CLI", Command: "curl https://cursor.com/install -fsS | bash"}, {Description: "Rerun setup", Command: "humansh setup"}}
			}
		case llm.OpenRouter:
			actions = []llm.DiagnosticAction{{Description: "Configure OpenRouter", Command: "humansh provider configure openrouter --model provider/model"}, {Description: "Rerun setup", Command: "humansh setup"}}
		}
	}
	for index, action := range actions {
		label := "Next"
		if index > 0 {
			label = "Then"
		}
		fmt.Fprintf(ui.streams.Out, "  %s %s\n", ui.paint(ansiBold, label+":"), ui.paint(ansiCyan, action.Command))
	}
}

func (ui *setupUI) prompt(label, defaultValue string) (string, error) {
	fmt.Fprintf(ui.streams.Out, "  %s %s: ", label, ui.paint(ansiDim, "["+defaultValue+"]"))
	line, err := ui.readString('\n')
	if err != nil && len(line) == 0 {
		return "", err
	}
	return strings.TrimSpace(strings.TrimSuffix(strings.TrimSuffix(line, "\n"), "\r")), nil
}

func (ui *setupUI) promptSecret(label string) (string, error) {
	fmt.Fprintf(ui.streams.Out, "  %s: ", label)
	if file, ok := ui.streams.In.(*os.File); ok && term.IsTerminal(int(file.Fd())) {
		return ui.promptTerminalSecret(file)
	}

	const maxBytes = 16 << 10
	value := make([]byte, 0, 64)
	for {
		next, err := ui.readByte()
		if err != nil {
			if err == io.EOF {
				return string(value), nil
			}
			return "", err
		}
		if next == '\n' {
			if len(value) > 0 && value[len(value)-1] == '\r' {
				value = value[:len(value)-1]
			}
			return string(value), nil
		}
		if len(value) == maxBytes {
			return "", fmt.Errorf("secret input exceeds 16 KiB")
		}
		value = append(value, next)
	}
}

func (ui *setupUI) promptTerminalSecret(file *os.File) (string, error) {
	state, err := term.MakeRaw(int(file.Fd()))
	if err != nil {
		return "", err
	}
	defer func() { _ = term.Restore(int(file.Fd()), state) }()

	const maxBytes = 16 << 10
	value := make([]byte, 0, 64)
	for {
		next, err := ui.readByte()
		if err != nil {
			fmt.Fprint(ui.streams.Out, "\r\n")
			return "", err
		}
		switch next {
		case '\r', '\n':
			fmt.Fprint(ui.streams.Out, "\r\n")
			return string(value), nil
		case 3:
			fmt.Fprint(ui.streams.Out, "^C\r\n")
			return "", context.Canceled
		case 4:
			if len(value) == 0 {
				fmt.Fprint(ui.streams.Out, "^D\r\n")
				return "", io.EOF
			}
		case 8, 127:
			if len(value) > 0 {
				value = value[:len(value)-1]
			}
		default:
			if len(value) == maxBytes {
				fmt.Fprint(ui.streams.Out, "\r\n")
				return "", fmt.Errorf("secret input exceeds 16 KiB")
			}
			value = append(value, next)
		}
	}
}

func (ui *setupUI) promptBinding(label, defaultValue string) (string, error) {
	file, ok := ui.streams.In.(*os.File)
	if !ui.interactive || !ok || !term.IsTerminal(int(file.Fd())) {
		return ui.prompt(label, defaultValue)
	}

	fmt.Fprintf(ui.streams.Out, "  %s %s: ", label, ui.paint(ansiDim, "["+defaultValue+"]"))
	state, err := term.MakeRaw(int(file.Fd()))
	if err != nil {
		return "", err
	}
	defer func() { _ = term.Restore(int(file.Fd()), state) }()

	var input []byte
	var echoes []string
	for {
		key, err := ui.readByte()
		if err != nil {
			fmt.Fprint(ui.streams.Out, "\r\n")
			return "", err
		}
		switch key {
		case '\r', '\n':
			fmt.Fprint(ui.streams.Out, "\r\n")
			return setupRawBindingInput(input)
		case 3: // Ctrl-C cancels setup instead of becoming a shortcut accidentally.
			fmt.Fprint(ui.streams.Out, "^C\r\n")
			return "", context.Canceled
		case 4: // Ctrl-D on an empty field is the terminal's conventional EOF.
			if len(input) == 0 {
				fmt.Fprint(ui.streams.Out, "^D\r\n")
				return "", io.EOF
			}
		case 8, 127:
			if len(input) == 0 {
				continue
			}
			input = input[:len(input)-1]
			echo := echoes[len(echoes)-1]
			echoes = echoes[:len(echoes)-1]
			for range echo {
				fmt.Fprint(ui.streams.Out, "\b \b")
			}
			continue
		}

		input = append(input, key)
		echo := string([]byte{key})
		if key < 32 || key == 127 {
			echo = "<" + setupControlKeyLabel(key) + ">"
		}
		echoes = append(echoes, echo)
		fmt.Fprint(ui.streams.Out, echo)
	}
}

func (ui *setupUI) askYesNo(label string, defaultYes bool) (bool, error) {
	hint := "y/N"
	if defaultYes {
		hint = "Y/n"
	}
	for {
		answer, err := ui.prompt(label, hint)
		if err != nil {
			return false, err
		}
		switch strings.ToLower(answer) {
		case "":
			return defaultYes, nil
		case "y", "yes":
			return true, nil
		case "n", "no":
			return false, nil
		default:
			ui.warning("Please answer yes or no.")
		}
	}
}

type setupExecutableChoice struct {
	value string
	label string
}

func configureSetupClaudeExecutable(cfg *config.RuntimeConfig, ui *setupUI) (bool, error) {
	executables := discoverClaudeExecutables(os.Getenv("PATH"))
	if !ui.interactive || (len(executables) < 2 && cfg.Claude.Binary == "") {
		return false, nil
	}

	autoLabel := "Auto (PATH, then ~/.local/bin)"
	if len(executables) > 0 {
		autoLabel += " → " + strconv.Quote(executables[0])
	}
	choices := []setupExecutableChoice{{label: autoLabel}}
	for index, executable := range executables {
		if index == 0 && cfg.Claude.Binary != executable {
			continue
		}
		choices = append(choices, setupExecutableChoice{value: executable, label: strconv.Quote(executable)})
	}
	if cfg.Claude.Binary != "" && !containsExecutableChoice(choices, cfg.Claude.Binary) {
		choices = append(choices, setupExecutableChoice{value: cfg.Claude.Binary, label: strconv.Quote(cfg.Claude.Binary) + " (configured, not currently on PATH)"})
	}

	current := 0
	for index, choice := range choices {
		if choice.value == cfg.Claude.Binary {
			current = index
			break
		}
	}
	ui.note("Claude aliases are not used. Choose the executable humansh should run:")
	for index, choice := range choices {
		marker := " "
		if index == current {
			marker = "*"
		}
		fmt.Fprintf(ui.streams.Out, "    %s %d  %s\n", marker, index+1, choice.label)
	}
	for {
		answer, err := ui.prompt("Claude executable", strconv.Itoa(current+1))
		if err != nil {
			return false, err
		}
		selected := current
		switch {
		case answer == "":
		case strings.EqualFold(answer, "auto"):
			selected = 0
		default:
			parsed, parseErr := strconv.Atoi(answer)
			if parseErr == nil {
				if parsed < 1 || parsed > len(choices) {
					ui.warning(fmt.Sprintf("Choose a number from 1 to %d, type auto, or enter an absolute path.", len(choices)))
					continue
				}
				selected = parsed - 1
			} else {
				candidate := *cfg
				candidate.Claude.Binary = answer
				if err := candidate.Validate(); err != nil {
					ui.warning("That Claude executable path is invalid: " + err.Error())
					continue
				}
				if !isExecutableFile(answer) {
					ui.warning("That path is not an executable file. Nothing has been changed; choose another path.")
					continue
				}
				choices = append(choices, setupExecutableChoice{value: answer, label: strconv.Quote(answer)})
				selected = len(choices) - 1
			}
		}

		before := cfg.Claude.Binary
		cfg.Claude.Binary = choices[selected].value
		if before == cfg.Claude.Binary {
			return false, nil
		}
		if cfg.Claude.Binary == "" {
			ui.success("Claude executable reset to automatic selection (PATH, then ~/.local/bin).")
		} else {
			ui.success("Claude executable set to " + strconv.Quote(cfg.Claude.Binary) + ".")
		}
		return true, nil
	}
}

func configureSetupCursorExecutable(cfg *config.RuntimeConfig, ui *setupUI) (bool, error) {
	executables := discoverCursorExecutables(os.Getenv("PATH"))
	if !ui.interactive || (len(executables) < 2 && cfg.Cursor.Binary == "") {
		return false, nil
	}

	autoLabel := "Auto (cursor-agent, then agent)"
	if len(executables) > 0 {
		autoLabel += " → " + strconv.Quote(executables[0])
	}
	choices := []setupExecutableChoice{{label: autoLabel}}
	for index, executable := range executables {
		if index == 0 && cfg.Cursor.Binary != executable {
			continue
		}
		choices = append(choices, setupExecutableChoice{value: executable, label: strconv.Quote(executable)})
	}
	if cfg.Cursor.Binary != "" && !containsExecutableChoice(choices, cfg.Cursor.Binary) {
		choices = append(choices, setupExecutableChoice{value: cfg.Cursor.Binary, label: strconv.Quote(cfg.Cursor.Binary) + " (configured, not currently on PATH)"})
	}

	current := 0
	for index, choice := range choices {
		if choice.value == cfg.Cursor.Binary {
			current = index
			break
		}
	}
	ui.note("Cursor editor launchers and shell aliases are not used. Choose the CLI executable humansh should run:")
	for index, choice := range choices {
		marker := " "
		if index == current {
			marker = "*"
		}
		fmt.Fprintf(ui.streams.Out, "    %s %d  %s\n", marker, index+1, choice.label)
	}
	for {
		answer, err := ui.prompt("Cursor executable", strconv.Itoa(current+1))
		if err != nil {
			return false, err
		}
		selected := current
		switch {
		case answer == "":
		case strings.EqualFold(answer, "auto"):
			selected = 0
		default:
			parsed, parseErr := strconv.Atoi(answer)
			if parseErr == nil {
				if parsed < 1 || parsed > len(choices) {
					ui.warning(fmt.Sprintf("Choose a number from 1 to %d, type auto, or enter an absolute path.", len(choices)))
					continue
				}
				selected = parsed - 1
			} else {
				candidate := *cfg
				candidate.Cursor.Binary = answer
				if err := candidate.Validate(); err != nil {
					ui.warning("That Cursor executable path is invalid: " + err.Error())
					continue
				}
				if !isExecutableFile(answer) {
					ui.warning("That path is not an executable file. Nothing has been changed; choose another path.")
					continue
				}
				choices = append(choices, setupExecutableChoice{value: answer, label: strconv.Quote(answer)})
				selected = len(choices) - 1
			}
		}

		before := cfg.Cursor.Binary
		cfg.Cursor.Binary = choices[selected].value
		if before == cfg.Cursor.Binary {
			return false, nil
		}
		if cfg.Cursor.Binary == "" {
			ui.success("Cursor executable reset to automatic PATH selection.")
		} else {
			ui.success("Cursor executable set to " + strconv.Quote(cfg.Cursor.Binary) + ".")
		}
		return true, nil
	}
}

func discoverClaudeExecutables(pathValue string) []string {
	seen := make(map[string]bool)
	var executables []string
	for _, directory := range filepath.SplitList(pathValue) {
		if directory == "" {
			continue
		}
		if !filepath.IsAbs(directory) {
			absolute, err := filepath.Abs(directory)
			if err != nil {
				continue
			}
			directory = absolute
		}
		candidate := filepath.Clean(filepath.Join(directory, "claude"))
		if seen[candidate] || !isExecutableFile(candidate) || strings.ContainsAny(candidate, "\r\n") {
			continue
		}
		seen[candidate] = true
		executables = append(executables, candidate)
	}
	return executables
}

func discoverCursorExecutables(pathValue string) []string {
	seenPaths := make(map[string]bool)
	seenTargets := make(map[string]bool)
	var executables []string
	for _, name := range []string{"cursor-agent", "agent"} {
		for _, directory := range filepath.SplitList(pathValue) {
			if directory == "" {
				continue
			}
			if !filepath.IsAbs(directory) {
				absolute, err := filepath.Abs(directory)
				if err != nil {
					continue
				}
				directory = absolute
			}
			candidate := filepath.Clean(filepath.Join(directory, name))
			if seenPaths[candidate] || !isExecutableFile(candidate) || strings.ContainsAny(candidate, "\r\n") {
				continue
			}
			seenPaths[candidate] = true
			target := candidate
			if resolved, err := filepath.EvalSymlinks(candidate); err == nil {
				target = resolved
			}
			if seenTargets[target] {
				continue
			}
			seenTargets[target] = true
			executables = append(executables, candidate)
		}
	}
	return executables
}

func isExecutableFile(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.Mode().IsRegular() && info.Mode().Perm()&0o111 != 0
}

func containsExecutableChoice(choices []setupExecutableChoice, value string) bool {
	for _, choice := range choices {
		if choice.value == value {
			return true
		}
	}
	return false
}

func configureSetupTranslation(paths config.Paths, cfg *config.RuntimeConfig, providerReady bool, ui *setupUI) error {
	defaults := config.Default()
	ui.setting("Directory context", setupContextLabel(cfg.WorkingContext), setupContextLabel(defaults.WorkingContext), "Controls how much of the working directory is shared with the provider.")
	ui.setting("Provider timeout", fmt.Sprintf("%d seconds", int(cfg.Timeout.Seconds())), fmt.Sprintf("%d seconds", int(defaults.Timeout.Seconds())), "Allowed range: 3–60 seconds.")
	ui.setting("Ambiguous input", "Ask before translating", "Ask before translating", "Fixed safety policy; the original text stays editable.")

	if ui.interactive {
		ui.note("Directory context choices: none, basename, or full. Full may expose private path names.")
		for {
			answer, err := ui.prompt("Directory context", cfg.WorkingContext)
			if err != nil {
				return err
			}
			if answer == "" {
				break
			}
			candidate := *cfg
			candidate.WorkingContext = strings.ToLower(answer)
			if err := candidate.Validate(); err != nil {
				ui.warning("Choose none, basename, or full.")
				continue
			}
			*cfg = candidate
			break
		}
		for {
			answer, err := ui.prompt("Provider timeout in seconds", strconv.Itoa(int(cfg.Timeout.Seconds())))
			if err != nil {
				return err
			}
			if answer == "" {
				break
			}
			seconds, err := strconv.Atoi(answer)
			if err != nil {
				ui.warning("Enter a whole number from 3 to 60.")
				continue
			}
			candidate := *cfg
			candidate.Timeout = time.Duration(seconds) * time.Second
			if err := candidate.Validate(); err != nil {
				ui.warning("Enter a whole number from 3 to 60.")
				continue
			}
			*cfg = candidate
			break
		}
	}

	if !providerReady {
		ui.setting("Provider model", "Not configured", "", "Choose a provider first; its built-in default will be used unless configured later.")
		return nil
	}
	return configureSetupModel(paths, cfg, ui)
}

func configureSetupModel(paths config.Paths, cfg *config.RuntimeConfig, ui *setupUI) error {
	model := setupModel(cfg)
	current := model
	if current == "" {
		current = "Provider default"
	}
	detail := "Leave blank to keep the current choice; type default to use the provider's built-in model."
	detected := ""
	if cfg.Provider == llm.Codex {
		var err error
		detected, err = config.ReadCodexSelectedModel(paths)
		if err != nil {
			ui.warning("Codex model preference could not be read safely; the provider default remains available.")
			detected = ""
		} else if detected != "" {
			detail += " Type detected to copy " + detected + "."
		}
	}
	ui.setting("Provider model", current, "Provider default", detail)
	if !ui.interactive || cfg.Provider == llm.OpenRouter {
		if cfg.Provider == llm.OpenRouter {
			ui.note("This OpenRouter model passed the required strict-schema probe during provider setup.")
		}
		return nil
	}

	for {
		answer, err := ui.prompt("Provider model", current)
		if err != nil {
			return err
		}
		if answer == "" {
			return nil
		}
		if strings.EqualFold(answer, "default") {
			answer = ""
		} else if strings.EqualFold(answer, "detected") && detected != "" {
			answer = detected
		}
		candidate := *cfg
		setSetupModel(&candidate, answer)
		if err := candidate.Validate(); err != nil {
			ui.warning("That model name is invalid: " + err.Error())
			continue
		}
		*cfg = candidate
		return nil
	}
}

func configureSetupShell(cfg *config.RuntimeConfig, shells []shell.ID, ui *setupUI) error {
	defaults := config.Default()
	hasZsh, hasBash := containsShell(shells, shell.Zsh), containsShell(shells, shell.Bash)
	if hasZsh {
		ui.setting("Zsh Enter", setupEnterBehavior(cfg.Shell.SmartEnter), setupEnterBehavior(defaults.Shell.SmartEnter), "")
	}
	if hasBash {
		if !hasZsh {
			cfg.Shell.SmartEnter = false
		}
		ui.setting("Bash Enter", "Runs as typed", "", "Use the translate shortcut for English requests.")
	}
	ui.setting("Clear input", config.BindingLabel(cfg.Shell.ClearLineBinding), config.BindingLabel(defaults.Shell.ClearLineBinding), "")
	ui.setting("Translate request", config.BindingLabel(cfg.Shell.ForceTranslateBinding), config.BindingLabel(defaults.Shell.ForceTranslateBinding), "")
	ui.setting("Run as typed", config.BindingLabel(cfg.Shell.ForceLiteralBinding), config.BindingLabel(defaults.Shell.ForceLiteralBinding), "Also confirms a translated command marked high risk.")

	var shortcutsInUse []string
	for _, binding := range []string{cfg.Shell.ClearLineBinding, cfg.Shell.ForceTranslateBinding, cfg.Shell.ForceLiteralBinding} {
		if setupShortcutCollision(binding) != "" {
			shortcutsInUse = append(shortcutsInUse, config.BindingLabel(binding))
		}
	}
	if len(shortcutsInUse) > 0 {
		ui.warning(setupShortcutList(shortcutsInUse) + " may already be used by your shell or terminal. Choose different shortcuts below if needed.")
	}
	if !ui.interactive {
		return nil
	}

	if hasZsh {
		smart, err := ui.askYesNo("Detect commands and requests when you press Enter?", cfg.Shell.SmartEnter)
		if err != nil {
			return err
		}
		cfg.Shell.SmartEnter = smart
	}
	ui.note("To change a shortcut, press the keys, then Enter. If it includes Enter, type its name instead (for example, Ctrl-X Enter).")
	if err := promptSetupBinding(ui, cfg, "clear"); err != nil {
		return err
	}
	if err := promptSetupBinding(ui, cfg, "translate"); err != nil {
		return err
	}
	if err := promptSetupBinding(ui, cfg, "literal"); err != nil {
		return err
	}
	return nil
}

func setupEnterBehavior(smart bool) string {
	if smart {
		return "Detects commands and requests"
	}
	return "Runs as typed"
}

func setupShortcutList(shortcuts []string) string {
	if len(shortcuts) == 1 {
		return shortcuts[0]
	}
	if len(shortcuts) == 2 {
		return shortcuts[0] + " and " + shortcuts[1]
	}
	return strings.Join(shortcuts[:len(shortcuts)-1], ", ") + ", and " + shortcuts[len(shortcuts)-1]
}

func promptSetupBinding(ui *setupUI, cfg *config.RuntimeConfig, bindingType string) error {
	label, current := "Clear input shortcut", cfg.Shell.ClearLineBinding
	switch bindingType {
	case "translate":
		label = "Translate request shortcut"
		current = cfg.Shell.ForceTranslateBinding
	case "literal":
		label = "Run as typed shortcut"
		current = cfg.Shell.ForceLiteralBinding
	}
	for {
		answer, err := ui.promptBinding(label, config.BindingLabel(current))
		if err != nil {
			return err
		}
		if answer == "" {
			return nil
		}
		binding, err := config.ParseBinding(answer)
		if err != nil {
			ui.warning(err.Error())
			continue
		}
		candidate := *cfg
		switch bindingType {
		case "translate":
			candidate.Shell.ForceTranslateBinding = binding
		case "literal":
			candidate.Shell.ForceLiteralBinding = binding
		default:
			candidate.Shell.ClearLineBinding = binding
		}
		if err := candidate.Validate(); err != nil {
			ui.warning("That shortcut conflicts with another setting: " + err.Error())
			continue
		}
		*cfg = candidate
		ui.success(label + " set to " + config.BindingLabel(binding) + ".")
		if warning := setupShortcutCollision(binding); warning != "" {
			ui.warning(warning)
		}
		return nil
	}
}

func setupRawBindingInput(input []byte) (string, error) {
	if len(input) == 0 {
		return "", nil
	}
	var value strings.Builder
	for _, key := range input {
		switch {
		case key == 27:
			value.WriteString("^[")
		case key == '\t':
			value.WriteString("^I")
		case key > 0 && key < 32:
			value.WriteByte('^')
			value.WriteByte('A' + key - 1)
		case key >= 32 && key < 127:
			value.WriteByte(key)
		default:
			return "", fmt.Errorf("that terminal key cannot be represented as a portable shell shortcut; type a name such as Ctrl-R instead")
		}
	}
	return value.String(), nil
}

func setupControlKeyLabel(key byte) string {
	switch key {
	case 9:
		return "Tab"
	case 27:
		return "Esc"
	case 28:
		return "Ctrl-\\"
	case 29:
		return "Ctrl-]"
	case 30:
		return "Ctrl-^"
	case 31:
		return "Ctrl-_"
	default:
		if key > 0 && key < 27 {
			return "Ctrl-" + string(rune('A'+key-1))
		}
		return fmt.Sprintf("0x%02X", key)
	}
}

func setupShortcutCollision(binding string) string {
	switch binding {
	case "^[":
		return "Esc will clear your input instead of performing its usual shell action."
	case "^G":
		return "Ctrl-G will replace its usual shell action and may already be used by your terminal."
	case "^R":
		return "Ctrl-R will no longer search your command history."
	case "^C":
		return "Ctrl-C will no longer cancel the current command."
	case "^Z":
		return "Ctrl-Z will no longer pause the current command."
	case "^L":
		return "Ctrl-L will no longer clear the terminal screen."
	case "^U":
		return "Ctrl-U will no longer clear from the cursor to the start of the line."
	case "^W":
		return "Ctrl-W will no longer clear the previous word."
	case "^X":
		return "Ctrl-X will run immediately instead of starting another shortcut."
	default:
		return ""
	}
}

func printSetupReview(cfg config.RuntimeConfig, shells []shell.ID, providerReady, noShellChange, newOpenRouterKey bool, ui *setupUI) {
	provider := "Not configured"
	if providerReady {
		provider = setupProviderName(cfg.Provider)
	}
	model := setupModel(&cfg)
	if model == "" {
		model = "Provider default"
	}
	startupNames := make([]string, 0, len(shells))
	for _, id := range shells {
		startupNames = append(startupNames, "~/"+shellStartupName(id))
	}
	startup := "Update " + strings.Join(startupNames, " and ")
	if noShellChange {
		startup = "Print managed blocks; do not edit startup files"
	}
	ui.setting("Provider", provider, "", "")
	if cfg.Provider == llm.Claude || cfg.Claude.Binary != "" {
		executable := cfg.Claude.Binary
		if executable == "" {
			executable = "Auto (PATH, then ~/.local/bin)"
		}
		ui.setting("Claude executable", executable, "Auto (PATH, then ~/.local/bin)", "Shell aliases are not used.")
	}
	if cfg.Provider == llm.Cursor || cfg.Cursor.Binary != "" {
		executable := cfg.Cursor.Binary
		if executable == "" {
			executable = "Auto (cursor-agent, then agent)"
		}
		ui.setting("Cursor executable", executable, "Auto (cursor-agent, then agent)", "The Cursor editor launcher is not used.")
	}
	ui.setting("Model", model, "Provider default", "")
	if cfg.Provider == llm.OpenRouter && providerReady {
		switch {
		case newOpenRouterKey:
			ui.setting("OpenRouter key", "New key — save after confirmation", "", "The value will not be displayed.")
		case os.Getenv("OPENROUTER_API_KEY") != "":
			ui.setting("OpenRouter key", "OPENROUTER_API_KEY from shell", "", "Humansh will use it without persisting it.")
		default:
			ui.setting("OpenRouter key", "Stored key (unchanged)", "", "The value will not be displayed.")
		}
	}
	ui.setting("Directory context", setupContextLabel(cfg.WorkingContext), setupContextLabel(config.Default().WorkingContext), "")
	ui.setting("Timeout", fmt.Sprintf("%d seconds", int(cfg.Timeout.Seconds())), fmt.Sprintf("%d seconds", int(config.Default().Timeout.Seconds())), "")
	ui.setting("Shell integrations", shellNames(shells), "Auto-detected", "")
	if containsShell(shells, shell.Zsh) {
		ui.setting("Zsh Enter", setupEnterBehavior(cfg.Shell.SmartEnter), setupEnterBehavior(config.Default().Shell.SmartEnter), "")
	}
	if containsShell(shells, shell.Bash) {
		ui.setting("Bash Enter", "Runs as typed", "", "")
	}
	ui.setting("Clear input", config.BindingLabel(cfg.Shell.ClearLineBinding), config.BindingLabel(config.Default().Shell.ClearLineBinding), "")
	ui.setting("Translate request", config.BindingLabel(cfg.Shell.ForceTranslateBinding), config.BindingLabel(config.Default().Shell.ForceTranslateBinding), "")
	ui.setting("Run as typed", config.BindingLabel(cfg.Shell.ForceLiteralBinding), config.BindingLabel(config.Default().Shell.ForceLiteralBinding), "")
	ui.setting("Shell activation", startup, "", "These files load Humansh's interactive command-line controls when each shell starts.")
}

func containsShell(shells []shell.ID, wanted shell.ID) bool {
	for _, id := range shells {
		if id == wanted {
			return true
		}
	}
	return false
}

func setupContextLabel(value string) string {
	switch value {
	case "none":
		return "None"
	case "full":
		return "Full path"
	default:
		return "Folder name only"
	}
}

func setupProviderName(id llm.ProviderID) string {
	switch id {
	case llm.Codex:
		return "Codex"
	case llm.Claude:
		return "Claude Code"
	case llm.Cursor:
		return "Cursor CLI"
	case llm.OpenRouter:
		return "OpenRouter"
	default:
		return string(id)
	}
}

func setupModel(cfg *config.RuntimeConfig) string {
	switch cfg.Provider {
	case llm.Claude:
		return cfg.Claude.Model
	case llm.Cursor:
		return cfg.Cursor.Model
	case llm.OpenRouter:
		return cfg.OpenRouter.Model
	default:
		return cfg.Codex.Model
	}
}

func setSetupModel(cfg *config.RuntimeConfig, model string) {
	switch cfg.Provider {
	case llm.Claude:
		cfg.Claude.Model = model
	case llm.Cursor:
		cfg.Cursor.Model = model
	case llm.OpenRouter:
		cfg.OpenRouter.Model = model
		cfg.OpenRouter.StructuredOutputProven = false
		cfg.OpenRouter.StructuredOutputModel = ""
	default:
		cfg.Codex.Model = model
	}
}

func setupBindingKeys(binding string) []string {
	label := config.BindingLabel(binding)
	if label == "" {
		return nil
	}
	return strings.Split(label, " then ")
}
