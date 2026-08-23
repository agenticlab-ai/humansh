package risk

import (
	"path"
	"regexp"
	"strings"

	"mvdan.cc/sh/v3/syntax"
)

type Level string

const (
	Low    Level = "low"
	Medium Level = "medium"
	High   Level = "high"
)

type Result struct {
	Level   Level    `json:"level"`
	Reasons []string `json:"reasons"`
}

type Analyzer struct{}

func (Analyzer) Analyze(command string) Result { return Analyze(command) }

type findingSet struct {
	level Level
	seen  map[string]bool
}

var reasonOrder = []string{
	"recursive_or_forced_deletion",
	"destructive_find",
	"destructive_xargs",
	"download_and_execute",
	"disk_or_filesystem_destruction",
	"recursive_permission_change_broad_path",
	"destructive_git",
	"infrastructure_destruction",
	"database_destruction",
	"user_or_account_deletion",
	"security_control_disabled",
	"encoded_or_obfuscated_execution",
	"resource_exhaustion",
	"system_disruption",
	"privilege_escalation",
	"file_deletion",
	"file_overwrite",
	"file_move_or_copy",
	"permission_or_ownership_change",
	"package_installation",
	"process_signal",
	"git_state_change",
	"authenticated_network_write",
	"remote_execution",
	"infrastructure_change",
	"database_write_or_migration",
	"shell_configuration_change",
	"nested_shell",
}

var (
	downloadPipe   = regexp.MustCompile(`(?i)\b(?:curl|wget)\b[^|;]*\|\s*(?:sudo\s+)?(?:command\s+)?(?:sh|bash|zsh)\b`)
	encodedPipe    = regexp.MustCompile(`(?i)\b(?:base64\s+(?:--decode|-d)|openssl\s+base64\s+-d)\b[^|;]*\|\s*(?:sudo\s+)?(?:sh|bash|zsh)\b`)
	printfPipe     = regexp.MustCompile(`(?i)\bprintf\b[^|;]*(?:\\x[0-9a-f]{2}|[A-Za-z0-9+/]{24,}={0,2})[^|;]*\|\s*(?:sh|bash|zsh)\b`)
	forkBomb       = regexp.MustCompile(`:\(\)\s*\{\s*:\s*\|\s*:\s*&\s*\}\s*;\s*:`)
	endlessWrite   = regexp.MustCompile(`(?i)(?:^|[;&|]\s*)yes(?:\s+[^;|]*)?\s*>`)
	shellConfig    = regexp.MustCompile(`(?i)(?:^|[/[:space:]])\.(?:zshrc|zprofile|zlogin|zlogout|bashrc|bash_profile|profile)(?:$|[[:space:];])`)
	databaseDrop   = regexp.MustCompile(`\b(?:drop\s+(?:database|table)|truncate\s+(?:table\s+)?)\b`)
	databaseWrite  = regexp.MustCompile(`\b(?:insert|update|delete|alter|create|replace|grant|revoke)\b`)
	encodedLiteral = regexp.MustCompile(`(?i)(?:\\x[0-9a-f]{2}|[A-Za-z0-9+/]{24,}={0,2})`)
	fallbackHigh   = []struct {
		reason  string
		pattern *regexp.Regexp
	}{
		{"recursive_or_forced_deletion", regexp.MustCompile(`(?i)(?:^|[;&|]\s*)(?:(?:sudo|command|env)\s+)*rm\s+[^\n]*(?:-[A-Za-z]*[rf][A-Za-z]*|--recursive|--force)`)},
		{"destructive_find", regexp.MustCompile(`(?i)\bfind\b[^\n]*(?:-delete|-exec\s+rm\b)`)},
		{"destructive_xargs", regexp.MustCompile(`(?i)\bxargs\b[^\n]*\brm\b`)},
		{"disk_or_filesystem_destruction", regexp.MustCompile(`(?i)\b(?:mkfs(?:\.[a-z0-9]+)?|fdisk|parted|diskutil\s+(?:erase|partition)|dd\s+[^\n]*\bof=/dev/)`)},
		{"destructive_git", regexp.MustCompile(`(?i)\bgit\s+(?:reset\s+--hard|clean\s+[^\n]*-[^\s]*f|push\s+[^\n]*(?:--force|-f\b))`)},
		{"infrastructure_destruction", regexp.MustCompile(`(?i)\b(?:terraform\s+destroy|kubectl\s+(?:delete|drain)|(?:aws|gcloud|az)\s+[^\n]*(?:delete|destroy|remove|terminate))\b`)},
		{"database_destruction", regexp.MustCompile(`(?i)\b(?:drop\s+(?:database|table)|truncate\s+(?:table\s+)?)\b`)},
		{"user_or_account_deletion", regexp.MustCompile(`(?i)\b(?:userdel|deluser|remove-local-user)\b`)},
		{"security_control_disabled", regexp.MustCompile(`(?i)\b(?:ufw\s+disable|setenforce\s+0|csrutil\s+disable|spctl\s+--master-disable)\b`)},
	}
)

// Analyze performs a portable AST inspection when parsing succeeds, then applies
// narrow raw-text checks for cross-pipeline and resource-exhaustion forms. A
// portable parse failure never lowers or rejects a Zsh-valid command; conservative
// fallback patterns still recognize the most consequential operations.
func Analyze(command string) Result {
	findings := findingSet{level: Low, seen: make(map[string]bool)}
	parsed := inspectScript(command, &findings, 0)
	inspectCrossCommandPatterns(maskQuoted(command), &findings)
	if !parsed {
		for _, item := range fallbackHigh {
			if item.pattern.MatchString(command) {
				findings.add(High, item.reason)
			}
		}
	}
	if shellConfig.MatchString(command) {
		findings.add(Medium, "shell_configuration_change")
	}

	reasons := make([]string, 0, len(findings.seen))
	for _, reason := range reasonOrder {
		if findings.seen[reason] {
			reasons = append(reasons, reason)
		}
	}
	return Result{Level: findings.level, Reasons: reasons}
}

func inspectScript(command string, findings *findingSet, depth int) bool {
	if depth > 3 {
		findings.add(High, "encoded_or_obfuscated_execution")
		return true
	}
	file, err := syntax.NewParser(syntax.Variant(syntax.LangBash)).Parse(strings.NewReader(command), "")
	if err != nil {
		return false
	}
	syntax.Walk(file, func(node syntax.Node) bool {
		switch typed := node.(type) {
		case *syntax.CallExpr:
			inspectCall(typed, findings, depth)
		case *syntax.Redirect:
			inspectRedirect(typed, findings)
		case *syntax.BinaryCmd:
			inspectPipeline(typed, findings)
		case *syntax.Stmt:
			inspectResourceRedirect(typed, findings)
		}
		return true
	})
	return true
}

func inspectCall(call *syntax.CallExpr, findings *findingSet, depth int) {
	words := make([]string, len(call.Args))
	for index, word := range call.Args {
		words[index], _ = staticWord(word)
	}
	if len(words) == 0 || words[0] == "" {
		return
	}
	name, args, usedSudo := unwrap(words)
	if usedSudo {
		findings.add(Medium, "privilege_escalation")
	}
	if name == "" {
		return
	}

	switch name {
	case "rm":
		if hasFlag(args, "r", "recursive") || hasFlag(args, "f", "force") {
			findings.add(High, "recursive_or_forced_deletion")
		} else {
			findings.add(Medium, "file_deletion")
		}
	case "rmdir", "unlink":
		findings.add(Medium, "file_deletion")
	case "find":
		if containsArg(args, "-delete") || findExecutes(args, "rm") {
			findings.add(High, "destructive_find")
		}
	case "xargs":
		if xargsExecutes(args, "rm") {
			findings.add(High, "destructive_xargs")
		}
	case "dd", "mkfs", "mkfs.ext2", "mkfs.ext3", "mkfs.ext4", "mkfs.xfs", "fdisk", "parted":
		findings.add(High, "disk_or_filesystem_destruction")
	case "diskutil":
		if anyPrefix(args, "erase", "partition") {
			findings.add(High, "disk_or_filesystem_destruction")
		}
	case "chmod", "chown", "chgrp":
		if hasFlag(args, "R", "recursive") && hasBroadTarget(args) {
			findings.add(High, "recursive_permission_change_broad_path")
		} else {
			findings.add(Medium, "permission_or_ownership_change")
		}
	case "mv", "cp", "install", "rsync":
		findings.add(Medium, "file_move_or_copy")
	case "kill", "killall", "pkill":
		findings.add(Medium, "process_signal")
	case "shutdown", "reboot", "halt", "poweroff":
		findings.add(High, "system_disruption")
	case "brew", "apt", "apt-get", "dnf", "yum", "npm", "pnpm", "yarn", "pip", "pip3", "pipx", "cargo", "gem", "go":
		if packageChanges(args) {
			findings.add(Medium, "package_installation")
		}
	case "git":
		inspectGit(args, findings)
	case "curl", "wget":
		if networkWrites(args) {
			findings.add(Medium, "authenticated_network_write")
		}
	case "gh":
		if ghWrites(args) {
			findings.add(Medium, "authenticated_network_write")
		}
	case "ssh", "scp", "sftp":
		findings.add(Medium, "remote_execution")
	case "terraform", "kubectl", "aws", "gcloud", "az", "docker":
		inspectInfrastructure(name, args, findings)
	case "psql", "mysql", "sqlite", "sqlite3", "mongosh", "redis-cli":
		inspectDatabase(args, findings)
	case "prisma", "alembic", "goose", "migrate", "flyway", "rails", "rake":
		if containsFold(strings.Join(args, " "), "migrate", "upgrade", "db:schema:load") {
			findings.add(Medium, "database_write_or_migration")
		}
	case "userdel", "deluser", "remove-local-user":
		findings.add(High, "user_or_account_deletion")
	case "dscl":
		if containsFold(strings.Join(args, " "), "-delete") && containsFold(strings.Join(args, " "), "/users/") {
			findings.add(High, "user_or_account_deletion")
		}
	case "ufw":
		if firstArg(args) == "disable" {
			findings.add(High, "security_control_disabled")
		}
	case "setenforce":
		if firstArg(args) == "0" {
			findings.add(High, "security_control_disabled")
		}
	case "csrutil":
		if firstArg(args) == "disable" {
			findings.add(High, "security_control_disabled")
		}
	case "spctl":
		if containsArg(args, "--master-disable") || containsArg(args, "--global-disable") {
			findings.add(High, "security_control_disabled")
		}
	case "stress", "stress-ng":
		findings.add(High, "resource_exhaustion")
	case "sh", "bash", "zsh":
		if script, ok := commandString(args); ok {
			findings.add(Medium, "nested_shell")
			if !inspectScript(script, findings, depth+1) {
				inspectCrossCommandPatterns(maskQuoted(script), findings)
			}
		}
	case "eval":
		findings.add(High, "encoded_or_obfuscated_execution")
	}
}

func inspectPipeline(binary *syntax.BinaryCmd, findings *findingSet) {
	if binary.Op != syntax.Pipe && binary.Op != syntax.PipeAll {
		return
	}
	var calls []*syntax.CallExpr
	flattenPipeline(binary, &calls)
	if len(calls) < 2 {
		return
	}
	lastName, _, _ := callParts(calls[len(calls)-1])
	if lastName != "sh" && lastName != "bash" && lastName != "zsh" {
		return
	}
	for _, call := range calls[:len(calls)-1] {
		name, args, _ := callParts(call)
		switch name {
		case "curl", "wget":
			findings.add(High, "download_and_execute")
		case "base64":
			if hasFlag(args, "d", "decode") {
				findings.add(High, "encoded_or_obfuscated_execution")
			}
		case "openssl":
			if containsFold(strings.Join(args, " "), "base64") && hasFlag(args, "d", "decode") {
				findings.add(High, "encoded_or_obfuscated_execution")
			}
		case "printf":
			if encodedLiteral.MatchString(strings.Join(args, " ")) {
				findings.add(High, "encoded_or_obfuscated_execution")
			}
		}
	}
}

func flattenPipeline(command syntax.Command, calls *[]*syntax.CallExpr) {
	switch typed := command.(type) {
	case *syntax.BinaryCmd:
		if typed.Op == syntax.Pipe || typed.Op == syntax.PipeAll {
			flattenPipeline(typed.X.Cmd, calls)
			flattenPipeline(typed.Y.Cmd, calls)
		}
	case *syntax.CallExpr:
		*calls = append(*calls, typed)
	}
}

func inspectResourceRedirect(statement *syntax.Stmt, findings *findingSet) {
	call, ok := statement.Cmd.(*syntax.CallExpr)
	if !ok || len(statement.Redirs) == 0 {
		return
	}
	name, _, _ := callParts(call)
	if name == "yes" {
		findings.add(High, "resource_exhaustion")
	}
}

func callParts(call *syntax.CallExpr) (name string, args []string, usedSudo bool) {
	words := make([]string, len(call.Args))
	for index, word := range call.Args {
		words[index], _ = staticWord(word)
	}
	if len(words) == 0 || words[0] == "" {
		return "", nil, false
	}
	return unwrap(words)
}

func inspectRedirect(redirect *syntax.Redirect, findings *findingSet) {
	switch redirect.Op {
	case syntax.RdrOut, syntax.AppOut, syntax.RdrInOut, syntax.RdrClob, syntax.AppClob, syntax.RdrAll, syntax.RdrAllClob, syntax.AppAll, syntax.AppAllClob:
		target, _ := staticWord(redirect.Word)
		if target != "/dev/null" {
			findings.add(Medium, "file_overwrite")
		}
	}
}

func inspectGit(args []string, findings *findingSet) {
	joined := strings.ToLower(strings.Join(args, " "))
	switch firstArg(args) {
	case "reset":
		if containsFold(joined, "--hard") {
			findings.add(High, "destructive_git")
		} else {
			findings.add(Medium, "git_state_change")
		}
	case "clean":
		if hasFlag(args[1:], "f", "force") {
			findings.add(High, "destructive_git")
		}
	case "push":
		if hasFlag(args[1:], "f", "force") || containsFold(joined, "--force-with-lease") {
			findings.add(High, "destructive_git")
		} else {
			findings.add(Medium, "git_state_change")
		}
	case "checkout", "restore":
		if containsBroadCheckout(args[1:]) {
			findings.add(High, "destructive_git")
		} else {
			findings.add(Medium, "git_state_change")
		}
	case "commit", "switch", "branch", "merge", "rebase", "tag", "revert", "cherry-pick":
		findings.add(Medium, "git_state_change")
	}
}

func inspectInfrastructure(name string, args []string, findings *findingSet) {
	joined := strings.ToLower(strings.Join(args, " "))
	destructive := false
	changes := false
	switch name {
	case "terraform":
		destructive = containsFold(joined, "destroy")
		changes = containsFold(joined, "apply", "import", "taint")
	case "kubectl":
		destructive = containsFold(joined, "delete", "drain")
		changes = containsFold(joined, "apply", "create", "patch", "replace", "scale", "rollout restart", "cordon")
	case "docker":
		destructive = containsFold(joined, "system prune", "volume prune", "volume rm")
		changes = containsFold(joined, " rm", " stop", " kill", " compose up", " compose down", " run") || anyPrefix(args, "rm", "stop", "kill", "run")
	default:
		destructive = containsFold(joined, "delete", "destroy", "remove", "terminate", "purge")
		changes = containsFold(joined, "create", "update", "put", "patch", "deploy", "start", "stop", "revoke")
	}
	if destructive {
		findings.add(High, "infrastructure_destruction")
	} else if changes {
		findings.add(Medium, "infrastructure_change")
	}
}

func inspectDatabase(args []string, findings *findingSet) {
	joined := strings.ToLower(strings.Join(args, " "))
	if databaseDrop.MatchString(joined) {
		findings.add(High, "database_destruction")
	} else if databaseWrite.MatchString(joined) {
		findings.add(Medium, "database_write_or_migration")
	}
}

func inspectCrossCommandPatterns(command string, findings *findingSet) {
	if downloadPipe.MatchString(command) {
		findings.add(High, "download_and_execute")
	}
	if encodedPipe.MatchString(command) || printfPipe.MatchString(command) {
		findings.add(High, "encoded_or_obfuscated_execution")
	}
	if forkBomb.MatchString(command) || endlessWrite.MatchString(command) {
		findings.add(High, "resource_exhaustion")
	}
}

func maskQuoted(command string) string {
	masked := []byte(command)
	var quote byte
	for index := 0; index < len(masked); index++ {
		current := masked[index]
		if quote == 0 {
			if current == '\'' || current == '"' {
				quote = current
			}
			if current == '\\' && index+1 < len(masked) {
				index++
			}
			continue
		}
		if current == quote {
			quote = 0
			continue
		}
		if quote == '"' && current == '\\' && index+1 < len(masked) {
			masked[index] = ' '
			index++
			masked[index] = ' '
			continue
		}
		masked[index] = ' '
	}
	return string(masked)
}

func unwrap(words []string) (name string, args []string, usedSudo bool) {
	index := 0
	for index < len(words) {
		name = commandBase(words[index])
		switch name {
		case "sudo":
			usedSudo = true
			index++
			index = skipWrapperOptions(words, index, map[string]bool{"-u": true, "--user": true, "-g": true, "--group": true, "-h": true, "--host": true, "-p": true, "--prompt": true, "-C": true, "--close-from": true, "-R": true, "--chroot": true, "-D": true, "--chdir": true})
		case "env":
			index++
			index = skipWrapperOptions(words, index, map[string]bool{"-u": true, "--unset": true, "-C": true, "--chdir": true, "-S": true, "--split-string": true})
			for index < len(words) && isAssignment(words[index]) {
				index++
			}
		case "command", "builtin", "nohup":
			index++
			for index < len(words) && strings.HasPrefix(words[index], "-") {
				index++
			}
		default:
			return name, words[index+1:], usedSudo
		}
	}
	return "", nil, usedSudo
}

func skipWrapperOptions(words []string, index int, takesValue map[string]bool) int {
	for index < len(words) && strings.HasPrefix(words[index], "-") {
		option := words[index]
		index++
		if takesValue[option] && index < len(words) {
			index++
		}
	}
	return index
}

func staticWord(word *syntax.Word) (string, bool) {
	if word == nil {
		return "", false
	}
	var builder strings.Builder
	for _, part := range word.Parts {
		if !appendStaticPart(&builder, part) {
			return "", false
		}
	}
	return builder.String(), true
}

func appendStaticPart(builder *strings.Builder, part syntax.WordPart) bool {
	switch typed := part.(type) {
	case *syntax.Lit:
		builder.WriteString(typed.Value)
	case *syntax.SglQuoted:
		builder.WriteString(typed.Value)
	case *syntax.DblQuoted:
		for _, nested := range typed.Parts {
			if !appendStaticPart(builder, nested) {
				return false
			}
		}
	default:
		return false
	}
	return true
}

func (findings *findingSet) add(level Level, reason string) {
	findings.seen[reason] = true
	if severity(level) > severity(findings.level) {
		findings.level = level
	}
}

func severity(level Level) int {
	switch level {
	case High:
		return 2
	case Medium:
		return 1
	default:
		return 0
	}
}

func commandBase(value string) string {
	return strings.ToLower(path.Base(value))
}

func hasFlag(args []string, short, long string) bool {
	for _, arg := range args {
		if arg == "--" {
			return false
		}
		if arg == "--"+long || strings.HasPrefix(arg, "--"+long+"=") {
			return true
		}
		if strings.HasPrefix(arg, "-") && !strings.HasPrefix(arg, "--") && strings.Contains(strings.TrimPrefix(arg, "-"), short) {
			return true
		}
	}
	return false
}

func containsArg(args []string, want string) bool {
	for _, arg := range args {
		if strings.EqualFold(arg, want) {
			return true
		}
	}
	return false
}

func containsFold(text string, values ...string) bool {
	text = strings.ToLower(text)
	for _, value := range values {
		if strings.Contains(text, strings.ToLower(value)) {
			return true
		}
	}
	return false
}

func anyPrefix(args []string, prefixes ...string) bool {
	for _, arg := range args {
		lower := strings.ToLower(arg)
		for _, prefix := range prefixes {
			if strings.HasPrefix(lower, strings.ToLower(prefix)) {
				return true
			}
		}
	}
	return false
}

func firstArg(args []string) string {
	if len(args) == 0 {
		return ""
	}
	return strings.ToLower(args[0])
}

func findExecutes(args []string, command string) bool {
	for index, arg := range args {
		if (arg == "-exec" || arg == "-execdir") && index+1 < len(args) && commandBase(args[index+1]) == command {
			return true
		}
	}
	return false
}

func xargsExecutes(args []string, command string) bool {
	for _, arg := range args {
		if commandBase(arg) == command {
			return true
		}
	}
	return false
}

func hasBroadTarget(args []string) bool {
	for _, arg := range args {
		if arg == "" || strings.HasPrefix(arg, "-") || isAssignment(arg) {
			continue
		}
		cleaned := strings.TrimRight(arg, "/")
		if cleaned == "" || cleaned == "." || cleaned == ".." || cleaned == "~" || strings.HasPrefix(cleaned, "~/") || strings.HasPrefix(cleaned, "$HOME") || strings.HasPrefix(cleaned, "${HOME}") || strings.ContainsAny(cleaned, "*?") {
			return true
		}
		for _, prefix := range []string{"/etc", "/usr", "/var", "/System", "/Library", "/Users", "/home"} {
			if cleaned == prefix || strings.HasPrefix(cleaned, prefix+"/") {
				return true
			}
		}
	}
	return false
}

func containsBroadCheckout(args []string) bool {
	for _, arg := range args {
		if arg == "." || arg == ".." || arg == ":/" || arg == "*" || arg == "--worktree" {
			return true
		}
	}
	return false
}

func packageChanges(args []string) bool {
	return containsFold(strings.Join(args, " "), "install", " add ", "uninstall", "remove", "upgrade", "update") || firstArg(args) == "add"
}

func networkWrites(args []string) bool {
	joined := strings.ToLower(strings.Join(args, " "))
	if containsFold(joined, "--data", "--form", "--upload-file", "--request post", "--request put", "--request patch", "--request delete", "-x post", "-x put", "-x patch", "-x delete") {
		return true
	}
	for _, arg := range args {
		if arg == "-d" || strings.HasPrefix(arg, "-d=") || (strings.HasPrefix(arg, "-d") && len(arg) > 2) || arg == "-F" || strings.HasPrefix(arg, "-F=") || (strings.HasPrefix(arg, "-F") && len(arg) > 2) || arg == "-T" || strings.HasPrefix(arg, "-T=") || (strings.HasPrefix(arg, "-T") && len(arg) > 2) {
			return true
		}
	}
	return false
}

func ghWrites(args []string) bool {
	joined := strings.ToLower(strings.Join(args, " "))
	return containsFold(joined, "--method post", "--method put", "--method patch", "--method delete", "-x post", "-x put", "-x patch", "-x delete") || containsFold(joined, "pr merge", "issue close", "release create")
}

func commandString(args []string) (string, bool) {
	for index, arg := range args {
		if arg == "-c" && index+1 < len(args) && args[index+1] != "" {
			return args[index+1], true
		}
	}
	return "", false
}

func isAssignment(value string) bool {
	index := strings.IndexByte(value, '=')
	return index > 0 && !strings.ContainsAny(value[:index], "/ \t\r\n")
}
