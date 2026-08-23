# Classification

`humansh` classifies intent, not shell syntax. English such as `find all files modified today` is syntactically a command invocation, so parsing alone cannot decide whether Enter should execute it.

The local classifier has three outcomes:

- `literal`: strong command evidence with weak English evidence. Zsh delegates to the prior Enter widget; Bash exposes the same result to machine callers while ordinary Enter remains native Readline behavior.
- `natural_language`: strong English evidence with weak command evidence. The selected provider is called and its command is inserted for review.
- `ambiguous`: conflicting or insufficient evidence. The buffer remains unchanged; no provider is called.

The thresholds are command `>= 5`, English `>= 5`, and weak conflict `<= 2`. Strong evidence on both sides is always ambiguous.

Before scoring, empty input, multiline buffers, and leading comments take the hard literal routes `empty_input`, `multiline_input`, and `leading_comment`. Exact command and English-prefix overrides take `configured_command_override` and `configured_english_prefix`; a conflict takes `conflicting_user_overrides`. These route labels are zero-weight decision evidence. The `DecisionCode` field itself always uses one of the four stable threshold labels: `strong_command_weak_english`, `strong_english_weak_command`, `conflicting_strong_evidence`, or `insufficient_evidence`.

## Command evidence

| Code | Weight | Normative trigger/example |
|---|---:|---|
| `resolved_first_token` | 5 | The active shell resolves the head as alias/function/builtin/reserved/command: `git status`. |
| `shell_operator` | 5 | Unquoted operator: `cat file | grep error`. |
| `explicit_command_path` | 5 | Head begins `/`, `./`, `../`, or `~/`: `./scripts/test`. |
| `assignment_prefix` | 5 | Leading shell assignment: `FOO=bar make`. |
| `shell_control_construct` | 5 | Control head such as `if`, `for`, `while`, `case`, or `function`. |
| `command_or_process_substitution` | 4 | Unquoted `$(...)`, backticks, `<(...)`, or `>(...)`. |
| `parameter_expansion` | 3 | Unquoted `$name` or `${name}`. |
| `conventional_flag` | 3 | Unquoted `-x` or `--long`: `ls -lah`. |
| `glob_syntax` | 3 | Unquoted glob syntax: `print **/*.go`; a sentence-ending `?` is excluded. |
| `quoted_argument` | 2 | Quoted argument after a resolved/path head: `echo 'show me files'`. |
| `path_argument` | 2 | Path-like argument: `cat ./README.md`. |

The active Zsh or Bash process supplies only the fixed first-token kind—alias, function, builtin, reserved, command, unresolved, empty, or unknown. The token itself stays on stdin. Resolution is evidence, never an unconditional verdict.

## English evidence

| Code | Weight | Normative trigger/example |
|---|---:|---|
| `natural_instruction_prefix` | 5 | Fixed request prefixes such as `show me`, `please`, `help me`, or `list the`. |
| `natural_question_prefix` | 5 | Fixed question prefixes, or grammatical `which … is/are/uses/using/listening`. |
| `question_mark` | 3 | Sentence-ending `?` that is not part of a glob. |
| `ordinary_sentence_structure` | 3 | Four or more ordinary words, an instruction/unresolved head, and no shell markers. |
| `natural_language_tail` | 4 | Resolved non-negative-list head plus a `grammar-tail-v1` word and no shell markers: `find all files modified today`. |
| `natural_clause` | 3 | A fixed clause such as `modified today`, gated by instruction/unresolved/tail evidence. |
| `unresolved_first_token` | 2 | The active shell reports `unresolved` or `unknown`; this alone never forces translation. |
| `mostly_ordinary_words` | 2 | A structurally English row whose tail is at least 75% alphabetic ordinary words. |
| `stopword_or_pronoun_density` | 1 | A structurally English row with at least two lexicon/stop words. |

The resolved-command tail lexicon is versioned as `grammar-tail-v1`:

```text
a all an any each every no some the this that these those whatever whichever
he her hers him his i it its me mine my our ours she their theirs them they us we what which who whom whose you your yours
am are be been being can could did do does had has have is may might must shall should was were will would
about after at before by during for from if in into of on over through to under until with without
```

The exact negative-list heads are:

```text
echo print printf man git docker kubectl npm pnpm yarn cargo brew gh humansh codex claude cursor cursor-agent agent
```

They do not receive `natural_language_tail`, and therefore cannot regain `natural_clause` or `mostly_ordinary_words` through a tail. For example, `docker ps that were running` remains literal. The corpus proves that `all` makes `find all files modified today` ambiguous and `it` makes `make it faster` ambiguous.

### When a real command's operands look like English

The tail rule is grammatical, not a list of known-chatty commands. That is what lets it catch `watch the logs` and `top processes by memory`, which no fixed command list would have covered. The cost is that a genuine command whose operands happen to be lexicon words is also ambiguous:

```text
mv a b               → ambiguous   ('a' is a determiner)
cp a b               → ambiguous
touch a b            → ambiguous
make all install     → ambiguous   ('all' is a quantifier)
time make all        → ambiguous
who am i             → ambiguous   ('am', 'i')
nano my notes        → ambiguous   ('my')
```

This is the safe direction, and deliberately so. A resolved head always scores command `5`, so these rows can never reach `natural_language`, which requires a command score of `2` or less — a real command is never silently rewritten or sent to a provider. The line is preserved untouched and nothing runs; you press the force-literal binding to execute it as typed.

If a command you run often lands here, add it as an exact override so it is always literal:

```sh
print -rn -- 'nano' | humansh classifier add-command
```

Force-translate still works on overridden commands, so nothing is lost. These rows are pinned in the corpus, so a change to the lexicon that moves any of them shows up as a test failure rather than as a surprise in someone's terminal.

The built-in clause fragments are `is using`, `are using`, `that were`, `in this folder`, `from the last`, `modified today`, `changed today`, `changed during`, `by size`, `by memory`, `is listening`, `were running`, `to the`, `if the`, and `it faster`. Prefix, clause, and tail lists use whole-word matching. Both score domains are additive; command `>=5` plus English `>=5` always fails toward `ambiguous`, even when one score is larger.

Use `humansh classify --json` to see stable scores, primary decision code, and ordered evidence. Raw input is omitted from output by default. The LLM never breaks a tie or authorizes execution.

Overrides live in `${XDG_CONFIG_HOME:-$HOME/.config}/humansh/classifier.toml`. Exact command overrides are case-sensitive; English prefixes are case-insensitive and whitespace-normalized. Changing weights, lexicon entries, negative-list entries, or normative examples requires corpus fixtures and release notes.

Add and remove overrides only through stdin:

```sh
print -rn -- 'deploy' | humansh classifier add-command
print -rn -- 'deploy' | humansh classifier remove-command
print -rn -- 'explain how to' | humansh classifier add-english-prefix
print -rn -- 'explain how to' | humansh classifier remove-english-prefix
```
