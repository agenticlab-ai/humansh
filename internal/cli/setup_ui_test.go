package cli

import (
	"bufio"
	"bytes"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/humansh/humansh/internal/config"
	"github.com/humansh/humansh/internal/llm"
	"github.com/humansh/humansh/internal/shell"
)

func interactiveSetupUI(input string, out, errOut *bytes.Buffer) *setupUI {
	streams := IO{In: strings.NewReader(input), Out: out, Err: errOut}
	return &setupUI{streams: streams, reader: bufio.NewReader(streams.In), interactive: true}
}

func TestSetupStartupPatchShowsManagedLinesOnly(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("PATH", "/usr/bin:/bin")
	dotfiles := home + "/dotfiles"
	if err := os.Mkdir(dotfiles, 0o700); err != nil {
		t.Fatal(err)
	}
	target := dotfiles + "/zshrc"
	if err := os.WriteFile(target, []byte("export PRIVATE_TOKEN=do-not-echo\n"), 0o640); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("dotfiles/zshrc", home+"/.zshrc"); err != nil {
		t.Fatal(err)
	}
	change, err := config.PreviewStartupChange(config.Paths{Binary: home + "/.local/bin/humansh", ShellDir: "/tmp/humansh-shell"}, config.Default(), false)
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	ui := newSetupUI(IO{Out: &out}, false)
	ui.startupPatch(change)
	text := out.String()
	for _, want := range []string{"Shell activation patch", "--- ~/.zshrc (before)", "+++ ~/.zshrc (after)", "+# >>> humansh >>>", "+source '/tmp/humansh-shell/humansh.zsh'", "symlink", "~/dotfiles/zshrc"} {
		if !strings.Contains(text, want) {
			t.Errorf("startup patch missing %q:\n%s", want, text)
		}
	}
	if strings.Contains(text, "PRIVATE_TOKEN") || strings.Contains(text, "do-not-echo") {
		t.Fatalf("startup patch exposed unrelated startup content:\n%s", text)
	}
}

func TestInteractiveSetupEditsEveryTranslationAndShellPreference(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	var out, errOut bytes.Buffer
	ui := interactiveSetupUI("full\n12\nn\nCtrl-U\nCtrl-T\nCtrl-X\n", &out, &errOut)

	if err := configureSetupTranslation(config.Paths{}, &cfg, false, ui); err != nil {
		t.Fatal(err)
	}
	if err := configureSetupShell(&cfg, []shell.ID{shell.Zsh}, ui); err != nil {
		t.Fatal(err)
	}
	if cfg.WorkingContext != "full" || cfg.Timeout.Seconds() != 12 || cfg.Shell.SmartEnter {
		t.Fatalf("preferences were not applied: %+v", cfg)
	}
	if cfg.Shell.ClearLineBinding != "^U" || cfg.Shell.ForceTranslateBinding != "^T" || cfg.Shell.ForceLiteralBinding != "^X" {
		t.Fatalf("friendly shortcuts were not parsed: %+v", cfg.Shell)
	}
	text := out.String()
	for _, want := range []string{"Directory context", "Provider timeout", "Smart Enter", "Clear command line", "Escape normally enters command mode", "Ctrl-G replaces stock send-break", "Ctrl-U normally erases", "Ctrl-X is a prefix", "Plain Ctrl-X runs immediately"} {
		if !strings.Contains(text, want) {
			t.Errorf("setup output missing %q:\n%s", want, text)
		}
	}
	if errOut.Len() != 0 {
		t.Fatalf("unexpected stderr: %s", errOut.String())
	}
}

func TestInteractiveSetupRejectsAnUnreachableShortcut(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	var out, errOut bytes.Buffer
	ui := interactiveSetupUI("\n\nCtrl-X\nCtrl-T\n\n", &out, &errOut)
	if err := configureSetupShell(&cfg, []shell.ID{shell.Zsh}, ui); err != nil {
		t.Fatal(err)
	}
	if cfg.Shell.ForceTranslateBinding != "^T" {
		t.Fatalf("replacement shortcut was not applied: %+v", cfg.Shell)
	}
	if !strings.Contains(out.String(), "cannot be prefixes of each other") {
		t.Fatalf("prefix conflict was not explained:\n%s", out.String())
	}
}

func TestInteractiveSetupCanCopyDetectedCodexModel(t *testing.T) {
	t.Parallel()
	path := t.TempDir() + "/config.toml"
	if err := os.WriteFile(path, []byte("model = \"gpt-test\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg := config.Default()
	var out, errOut bytes.Buffer
	ui := interactiveSetupUI("detected\n", &out, &errOut)
	if err := configureSetupModel(config.Paths{CodexConfigFile: path}, &cfg, ui); err != nil {
		t.Fatal(err)
	}
	if cfg.Codex.Model != "gpt-test" || !strings.Contains(out.String(), "Type detected to copy gpt-test") {
		t.Fatalf("model=%q out=%s", cfg.Codex.Model, out.String())
	}
}

func TestInteractiveSetupChoosesAndPersistsExactClaudeExecutable(t *testing.T) {
	firstDir := t.TempDir()
	secondDir := t.TempDir()
	for _, directory := range []string{firstDir, secondDir} {
		path := directory + "/claude"
		if err := os.WriteFile(path, []byte("test executable"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", firstDir+string(os.PathListSeparator)+secondDir)
	cfg := config.Default()
	var out, errOut bytes.Buffer
	ui := interactiveSetupUI("2\n", &out, &errOut)
	changed, err := configureSetupClaudeExecutable(&cfg, ui)
	if err != nil {
		t.Fatal(err)
	}
	want := secondDir + "/claude"
	if !changed || cfg.Claude.Binary != want {
		t.Fatalf("changed=%t binary=%q want=%q", changed, cfg.Claude.Binary, want)
	}
	for _, text := range []string{"Claude aliases are not used", firstDir + "/claude", secondDir + "/claude", "Claude executable set"} {
		if !strings.Contains(out.String(), text) {
			t.Errorf("setup chooser missing %q:\n%s", text, out.String())
		}
	}
}

func TestInteractiveSetupCanRestoreAutomaticClaudeSelection(t *testing.T) {
	directory := t.TempDir()
	path := directory + "/claude"
	if err := os.WriteFile(path, []byte("test executable"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", directory)
	cfg := config.Default()
	cfg.Claude.Binary = path
	var out, errOut bytes.Buffer
	ui := interactiveSetupUI("auto\n", &out, &errOut)
	changed, err := configureSetupClaudeExecutable(&cfg, ui)
	if err != nil {
		t.Fatal(err)
	}
	if !changed || cfg.Claude.Binary != "" || !strings.Contains(out.String(), "automatic selection (PATH, then ~/.local/bin)") {
		t.Fatalf("changed=%t binary=%q output=%s", changed, cfg.Claude.Binary, out.String())
	}
}

func TestInteractiveSetupChoosesAndPersistsExactCursorExecutable(t *testing.T) {
	firstDir := t.TempDir()
	secondDir := t.TempDir()
	for _, directory := range []string{firstDir, secondDir} {
		path := directory + "/cursor-agent"
		if err := os.WriteFile(path, []byte("test executable"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", firstDir+string(os.PathListSeparator)+secondDir)
	cfg := config.Default()
	var out, errOut bytes.Buffer
	ui := interactiveSetupUI("2\n", &out, &errOut)
	changed, err := configureSetupCursorExecutable(&cfg, ui)
	if err != nil {
		t.Fatal(err)
	}
	want := secondDir + "/cursor-agent"
	if !changed || cfg.Cursor.Binary != want {
		t.Fatalf("changed=%t binary=%q want=%q", changed, cfg.Cursor.Binary, want)
	}
	for _, text := range []string{"Cursor editor launchers", firstDir + "/cursor-agent", secondDir + "/cursor-agent", "Cursor executable set"} {
		if !strings.Contains(out.String(), text) {
			t.Errorf("setup chooser missing %q:\n%s", text, out.String())
		}
	}
}

func TestCursorExecutableDiscoveryPrefersCursorAgentAndDeduplicatesAgentAlias(t *testing.T) {
	directory := t.TempDir()
	target := directory + "/cursor-cli"
	if err := os.WriteFile(target, []byte("test executable"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, directory+"/cursor-agent"); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, directory+"/agent"); err != nil {
		t.Fatal(err)
	}
	got := discoverCursorExecutables(directory)
	want := []string{directory + "/cursor-agent"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("executables=%v want=%v", got, want)
	}
}

func TestStyledSetupHasVisibleOrientation(t *testing.T) {
	t.Parallel()
	var out, errOut bytes.Buffer
	ui := interactiveSetupUI("", &out, &errOut)
	ui.styled = true
	ui.header()
	ui.section(1, 6, "Shell compatibility")
	ui.status(true, "Zsh 5.9", "compatible")
	text := out.String()
	for _, want := range []string{"\x1b[", "humansh setup", "1/6  Shell compatibility", "✓"} {
		if !strings.Contains(text, want) {
			t.Fatalf("styled setup missing %q: %q", want, text)
		}
	}
}

func TestSetupLoaderUsesStableTextWhenOutputIsRedirected(t *testing.T) {
	t.Parallel()
	var out bytes.Buffer
	ui := newSetupUI(IO{Out: &out}, false)
	called := false

	ui.withLoader("Checking AI providers…", func() {
		called = true
	})

	if !called {
		t.Fatal("loader did not run its work")
	}
	if got, want := out.String(), "  … Checking AI providers…\n"; got != want {
		t.Fatalf("redirected loader output=%q want=%q", got, want)
	}
	if strings.ContainsAny(out.String(), "\r\x1b") {
		t.Fatalf("redirected loader emitted terminal controls: %q", out.String())
	}
}

func TestSetupLoaderAnimatesAndClearsOnATerminal(t *testing.T) {
	t.Parallel()
	var out bytes.Buffer
	ui := &setupUI{streams: IO{Out: &out}, animated: true}

	ui.withLoader("Checking installed shells…", func() {})

	text := out.String()
	if !strings.Contains(text, "\r  ⠋ Checking installed shells…") {
		t.Fatalf("terminal loader did not render its first frame: %q", text)
	}
	if !strings.HasSuffix(text, "\r") || strings.Contains(text, "\n") {
		t.Fatalf("terminal loader was not cleared in place: %q", text)
	}
}

func TestSetupSecretPromptDoesNotEchoOrConsumeTheNextAnswer(t *testing.T) {
	t.Parallel()
	var out, errOut bytes.Buffer
	ui := interactiveSetupUI("sk-or-hidden\nprovider/model\n", &out, &errOut)

	secret, err := ui.promptSecret("Paste OpenRouter API key (input hidden)")
	if err != nil {
		t.Fatal(err)
	}
	model, err := ui.prompt("OpenRouter model ID", "required")
	if err != nil {
		t.Fatal(err)
	}
	if secret != "sk-or-hidden" || model != "provider/model" {
		t.Fatalf("secret=%q model=%q", secret, model)
	}
	if strings.Contains(out.String(), secret) {
		t.Fatalf("secret prompt echoed the key: %s", out.String())
	}
}

func TestSetupReviewShowsStagedOpenRouterKeyWithoutItsValue(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.Provider = llm.OpenRouter
	cfg.OpenRouter.Model = "provider/model"
	cfg.OpenRouter.StructuredOutputProven = true
	cfg.OpenRouter.StructuredOutputModel = cfg.OpenRouter.Model
	var out bytes.Buffer
	ui := newSetupUI(IO{Out: &out}, false)

	printSetupReview(cfg, []shell.ID{shell.Zsh}, true, false, true, ui)

	text := out.String()
	if !strings.Contains(text, "OpenRouter key") || !strings.Contains(text, "New key — save after confirmation") || !strings.Contains(text, "value will not be displayed") {
		t.Fatalf("review did not explain staged credential:\n%s", text)
	}
}

func TestRawShortcutCaptureRepresentsPhysicalControlKeys(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		input []byte
		want  string
	}{
		{name: "Ctrl-R", input: []byte{18}, want: "^R"},
		{name: "Ctrl-X Ctrl-T", input: []byte{24, 20}, want: "^X^T"},
		{name: "Esc t", input: []byte{27, 't'}, want: "^[t"},
		{name: "typed friendly name", input: []byte("Ctrl-R"), want: "Ctrl-R"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := setupRawBindingInput(test.input)
			if err != nil {
				t.Fatal(err)
			}
			if got != test.want {
				t.Fatalf("setupRawBindingInput(%v)=%q, want %q", test.input, got, test.want)
			}
		})
	}
}

func TestCtrlRShortcutWarnsAboutHistorySearch(t *testing.T) {
	t.Parallel()
	warning := setupShortcutCollision("^R")
	if !strings.Contains(warning, "reverse history search") {
		t.Fatalf("Ctrl-R collision warning=%q", warning)
	}
}

func TestSetupProviderDiagnosticShowsSelectedBinaryAndNextSteps(t *testing.T) {
	t.Parallel()
	var out, errOut bytes.Buffer
	ui := interactiveSetupUI("", &out, &errOut)
	ui.providerDiagnostic(llm.Claude, llm.Diagnostic{
		Installed:     true,
		Authenticated: true,
		AuthMode:      "claude.ai",
		Executable:    "/first-on-path/claude",
		Version:       "2.1.168 (Claude Code)",
		Message:       "version is below the verified baseline",
		NextSteps: []llm.DiagnosticAction{
			{Description: "Update Claude Code", Command: "claude update"},
			{Description: "Recheck", Command: "humansh setup"},
		},
	})
	text := out.String()
	for _, want := range []string{"Claude Code", "Update needed", `Executable "/first-on-path/claude"`, "2.1.168", "Next:", "claude update", "Then:", "humansh setup"} {
		if !strings.Contains(text, want) {
			t.Errorf("provider diagnostic missing %q:\n%s", want, text)
		}
	}
}
