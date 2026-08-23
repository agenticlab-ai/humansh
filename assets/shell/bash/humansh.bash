# Embedded humansh Bash/Readline integration. Installed by `humansh setup --shell bash`.
# PROMPT_COMMAND is intentionally preserved in both its scalar and Bash 5 array forms.
# shellcheck shell=bash disable=SC2034,SC2155,SC2178

[[ $- == *i* ]] || return 0
[[ ${HUMANSH_DISABLE-0} == 1 ]] && return 0
[[ ${_HUMANSH_BASH_LOADED-0} == 1 ]] && return 0
if (( BASH_VERSINFO[0] < 4 || (BASH_VERSINFO[0] == 4 && BASH_VERSINFO[1] < 3) )); then
  printf '\nhumansh disabled: Bash 4.3 or newer is required to safely capture and restore Readline bindings. On macOS, install a current Bash and rerun humansh setup --shell bash.\n' >&2
  return 0
fi

: "${HUMANSH_SMART_ENTER:=0}"
: "${HUMANSH_CLEAR_LINE_BINDING:=^[}"
: "${HUMANSH_FORCE_TRANSLATE_BINDING:=^G}"
: "${HUMANSH_FORCE_LITERAL_BINDING:=^X^M}"
: "${HUMANSH_PROVIDER_LABEL:=provider}"

_humansh_activation_error() { printf '\n%s\n' "$1" >&2; }

_humansh_binding_valid() {
  local value=$1
  [[ -n $value && ${#value} -le 32 ]] || return 1
  case $value in *[!A-Za-z0-9^'['']'_+.,:/=-]*) return 1 ;; esac
}

if [[ $HUMANSH_SMART_ENTER != 0 ]]; then
  _humansh_activation_error 'humansh disabled: Bash uses safe explicit translation mode and requires HUMANSH_SMART_ENTER=0. Next: run humansh setup --shell bash --repair.'
  return 0
fi
if ! _humansh_binding_valid "$HUMANSH_CLEAR_LINE_BINDING" || ! _humansh_binding_valid "$HUMANSH_FORCE_TRANSLATE_BINDING" || ! _humansh_binding_valid "$HUMANSH_FORCE_LITERAL_BINDING"; then
  _humansh_activation_error 'humansh disabled: configured key binding is invalid. Next: run humansh doctor --fix.'
  return 0
fi
if [[ $HUMANSH_CLEAR_LINE_BINDING == "$HUMANSH_FORCE_TRANSLATE_BINDING" || $HUMANSH_CLEAR_LINE_BINDING == "$HUMANSH_FORCE_LITERAL_BINDING" || $HUMANSH_FORCE_TRANSLATE_BINDING == "$HUMANSH_FORCE_LITERAL_BINDING" ]]; then
  _humansh_activation_error 'humansh disabled: clear-line, force-translate, and force-literal bindings must differ. Next: run humansh doctor --fix.'
  return 0
fi
if [[ $HUMANSH_CLEAR_LINE_BINDING == "$HUMANSH_FORCE_TRANSLATE_BINDING"* || $HUMANSH_FORCE_TRANSLATE_BINDING == "$HUMANSH_CLEAR_LINE_BINDING"* ||
      $HUMANSH_CLEAR_LINE_BINDING == "$HUMANSH_FORCE_LITERAL_BINDING"* || $HUMANSH_FORCE_LITERAL_BINDING == "$HUMANSH_CLEAR_LINE_BINDING"* ||
      $HUMANSH_FORCE_TRANSLATE_BINDING == "$HUMANSH_FORCE_LITERAL_BINDING"* || $HUMANSH_FORCE_LITERAL_BINDING == "$HUMANSH_FORCE_TRANSLATE_BINDING"* ]]; then
  _humansh_activation_error 'humansh disabled: configured key bindings cannot be prefixes of each other. Next: run humansh setup to choose different shortcuts.'
  return 0
fi
case :$HUMANSH_CLEAR_LINE_BINDING:$HUMANSH_FORCE_TRANSLATE_BINDING:$HUMANSH_FORCE_LITERAL_BINDING: in
  *:'^M':*|*:'^J':*)
    _humansh_activation_error 'humansh disabled: a Bash action binding cannot be Enter by itself. Next: run humansh setup --shell bash to choose different shortcuts.'
    return 0 ;;
esac

_humansh_readline_sequence() {
  local notation=$1 result='' key control lower
  local i=0
  while (( i < ${#notation} )); do
    key=${notation:i:1}
    if [[ $key == '^' && $((i + 1)) -lt ${#notation} ]]; then
      ((i++))
      control=${notation:i:1}
      case $control in
        '[') result+='\e' ;;
        *)
          lower=$(printf '%s' "$control" | tr '[:upper:]' '[:lower:]')
          result+="\\C-$lower" ;;
      esac
    else
      result+=$key
    fi
    ((i++))
  done
  printf '%s' "$result"
}

_humansh_binding_label() {
  local notation=$1 label='' key control upper
  local i=0
  while (( i < ${#notation} )); do
    key=${notation:i:1}
    if [[ $key == '^' && $((i + 1)) -lt ${#notation} ]]; then
      ((i++))
      control=${notation:i:1}
      upper=$(printf '%s' "$control" | tr '[:lower:]' '[:upper:]')
      case $upper in
        M) key='Enter' ;;
        I) key='Tab' ;;
        '[') key='Esc' ;;
        *) key="Ctrl-$upper" ;;
      esac
    fi
    [[ -n $label ]] && label+=' then '
    label+=$key
    ((i++))
  done
  printf '%s' "$label"
}

# Encode the portable binding notation as comma-delimited byte values. A
# bind -x callback blocks ordinary Readline dispatch while it runs, so the
# active translation loop matches the configured clear shortcut itself.
_humansh_binding_codes() {
  local notation=$1 result='' key control upper code
  local i=0
  while (( i < ${#notation} )); do
    key=${notation:i:1}
    if [[ $key == '^' && $((i + 1)) -lt ${#notation} ]]; then
      ((i++))
      control=${notation:i:1}
      upper=${control^^}
      if [[ $upper == '[' ]]; then
        code=27
      else
        printf -v code '%d' "'$upper"
        (( code &= 31 ))
      fi
    else
      printf -v code '%d' "'$key"
    fi
    result+="${code},"
    ((i++))
  done
  printf '%s' "$result"
}

_HUMANSH_CLEAR_SEQUENCE=$(_humansh_readline_sequence "$HUMANSH_CLEAR_LINE_BINDING")
_HUMANSH_TRANSLATE_SEQUENCE=$(_humansh_readline_sequence "$HUMANSH_FORCE_TRANSLATE_BINDING")
_HUMANSH_LITERAL_SEQUENCE=$(_humansh_readline_sequence "$HUMANSH_FORCE_LITERAL_BINDING")
_HUMANSH_CLEAR_CODES=$(_humansh_binding_codes "$HUMANSH_CLEAR_LINE_BINDING")
_HUMANSH_FORCE_LITERAL_LABEL=$(_humansh_binding_label "$HUMANSH_FORCE_LITERAL_BINDING")
_HUMANSH_FORCE_TRANSLATE_LABEL=$(_humansh_binding_label "$HUMANSH_FORCE_TRANSLATE_BINDING")
_HUMANSH_KEYMAPS=(emacs-standard vi-insert vi-command)
_HUMANSH_PRIOR_CLEAR=()
_HUMANSH_PRIOR_TRANSLATE=()
_HUMANSH_PRIOR_LITERAL=()
_HUMANSH_PRIOR_ENTER_CR=()
_HUMANSH_PRIOR_ENTER_LF=()
_HUMANSH_PENDING_BUFFER=${_HUMANSH_PENDING_BUFFER-}
_HUMANSH_PENDING_RISK=${_HUMANSH_PENDING_RISK-}
_HUMANSH_ENTER_GATED=0
_HUMANSH_ACTIVE=0
_HUMANSH_WARNED_MISSING=0
_HUMANSH_CANCELLED_BY_CLEAR=0
_HUMANSH_DEFERRED_KEYS=''
_HUMANSH_PRIOR_PROMPT_COMMAND_WAS_SET=${PROMPT_COMMAND+x}
_HUMANSH_PRIOR_PROMPT_COMMAND=${PROMPT_COMMAND-}
_HUMANSH_PRIOR_PROMPT_COMMAND_IS_ARRAY=0
_HUMANSH_PRIOR_PROMPT_COMMAND_ARRAY=("${PROMPT_COMMAND[@]-}")
_HUMANSH_PRIOR_PROMPT_COMMAND_DECL=$(declare -p PROMPT_COMMAND 2>/dev/null || true)
[[ $_HUMANSH_PRIOR_PROMPT_COMMAND_DECL == 'declare -a '* ]] && _HUMANSH_PRIOR_PROMPT_COMMAND_IS_ARRAY=1
unset _HUMANSH_PRIOR_PROMPT_COMMAND_DECL

_humansh_message() {
  local message=${1//$'\n'/ }
  message=${message//$'\r'/ }
  message=${message//$'\e'/ }
  printf '\n%s\n' "${message:0:500}" >&2
}

_humansh_capture_binding() {
  local keymap=$1 sequence=$2 output line prefix
  prefix="\"$sequence\":"
  if output=$(bind -m "$keymap" -X 2>/dev/null); then
    while IFS= read -r line; do
      [[ $line == "$prefix"* ]] && { printf 'x:%s' "$line"; return; }
    done <<< "$output"
  fi
  while IFS= read -r line; do
    [[ $line == "$prefix"* ]] && { printf 'normal:%s' "$line"; return; }
  done < <(bind -m "$keymap" -p 2>/dev/null)
  while IFS= read -r line; do
    [[ $line == "$prefix"* ]] && { printf 'normal:%s' "$line"; return; }
  done < <(bind -m "$keymap" -s 2>/dev/null)
  printf 'none:'
}

_humansh_restore_binding() {
  local keymap=$1 sequence=$2 saved=$3 kind=${3%%:*} line=${3#*:}
  case $kind in
    x) bind -m "$keymap" -x "$line" ;;
    normal) bind -m "$keymap" "$line" ;;
    *) bind -m "$keymap" -r "$sequence" 2>/dev/null || true ;;
  esac
}

_humansh_bind_shell() {
  local keymap=$1 sequence=$2 callback=$3
  bind -m "$keymap" -x "\"$sequence\":$callback"
}

_humansh_first_token_kind() {
  local first='' rest='' kind=''
  read -r first rest <<< "$READLINE_LINE"
  [[ -n $first ]] || { printf empty; return; }
  kind=$(type -t -- "$first" 2>/dev/null) || { printf unresolved; return; }
  case $kind in
    alias) printf alias ;;
    function) printf function ;;
    builtin) printf builtin ;;
    keyword) printf reserved ;;
    file) printf command ;;
    *) printf unknown ;;
  esac
}

_humansh_disable_pending_gate() {
  [[ $_HUMANSH_ENTER_GATED == 1 ]] || return 0
  local i keymap
  for ((i = 0; i < ${#_HUMANSH_KEYMAPS[@]}; i++)); do
    keymap=${_HUMANSH_KEYMAPS[i]}
    _humansh_restore_binding "$keymap" '\C-m' "${_HUMANSH_PRIOR_ENTER_CR[i]}"
    _humansh_restore_binding "$keymap" '\C-j' "${_HUMANSH_PRIOR_ENTER_LF[i]}"
  done
  _HUMANSH_ENTER_GATED=0
}

_humansh_enable_pending_gate() {
  [[ $_HUMANSH_ENTER_GATED == 0 ]] || return 0
  local keymap
  for keymap in "${_HUMANSH_KEYMAPS[@]}"; do
    _humansh_bind_shell "$keymap" '\C-m' _humansh_blocked_enter
    _humansh_bind_shell "$keymap" '\C-j' _humansh_blocked_enter
  done
  _HUMANSH_ENTER_GATED=1
}

_humansh_prompt_reset() {
  if [[ -n $_HUMANSH_PENDING_BUFFER ]]; then
    _HUMANSH_PENDING_BUFFER=''
    _HUMANSH_PENDING_RISK=''
    _humansh_disable_pending_gate
  fi
}

_humansh_clear_translation_spinner() {
  local width=$(( ${#HUMANSH_PROVIDER_LABEL} + 31 ))
  printf '\r%*s\r' "$width" '' >&2
}

_humansh_run_translation_with_spinner() {
  local kind=$1 output_file=$2 error_file=$3 state_file=$4 provider_pid key='' candidate_keys='' candidate_codes='' deferred_keys=''
  local frames=(⠋ ⠙ ⠹ ⠸ ⠼ ⠴ ⠦ ⠧ ⠇ ⠏)
  local frame_index=0 exit_status=0 key_code=0 interrupted=0 cancelled_by_clear=0
  set +m
  trap 'interrupted=1; [[ -n $provider_pid ]] && kill -TERM "$provider_pid" 2>/dev/null' INT
  trap 'interrupted=1; [[ -n $provider_pid ]] && kill -TERM "$provider_pid" 2>/dev/null' HUP TERM

  printf '%s' "$READLINE_LINE" | command humansh translate --protocol readline-v1 --shell bash --first-token-kind "$kind" >"$output_file" 2>"$error_file" &
  provider_pid=$!
  printf '\n' >&2
  while kill -0 "$provider_pid" 2>/dev/null; do
    printf '\r  %s Translating with %s…' "${frames[frame_index]}" "$HUMANSH_PROVIDER_LABEL" >&2
    key=''
    if IFS= read -r -s -N 1 -t 0.08 key; then
      printf -v key_code '%d' "'$key"
      candidate_keys+=$key
      candidate_codes+="${key_code},"
      if [[ $candidate_codes == "$_HUMANSH_CLEAR_CODES" ]]; then
        cancelled_by_clear=1
        kill -TERM "$provider_pid" 2>/dev/null || true
        break
      fi
      if (( key_code == 3 )); then
        interrupted=1
        kill -TERM "$provider_pid" 2>/dev/null || true
        break
      fi
      if [[ $_HUMANSH_CLEAR_CODES != "$candidate_codes"* ]]; then
        [[ $candidate_keys != *[[:cntrl:]]* ]] && deferred_keys+=$candidate_keys
        candidate_keys=''
        candidate_codes=''
      fi
    fi
    frame_index=$(( (frame_index + 1) % ${#frames[@]} ))
  done
  [[ -n $candidate_keys && $candidate_keys != *[[:cntrl:]]* ]] && deferred_keys+=$candidate_keys
  if wait "$provider_pid"; then exit_status=0; else exit_status=$?; fi
  _humansh_clear_translation_spinner
  trap - HUP INT TERM
  if (( cancelled_by_clear )); then
    printf 'C' >"$state_file"
    return 130
  fi
  printf 'D%s' "$deferred_keys" >"$state_file"
  (( interrupted )) && return 130
  return "$exit_status"
}

_humansh_apply_deferred_keys() {
  local keys=$_HUMANSH_DEFERRED_KEYS prefix suffix
  _HUMANSH_DEFERRED_KEYS=''
  [[ -n $keys ]] || return 0
  prefix=${READLINE_LINE:0:READLINE_POINT}
  suffix=${READLINE_LINE:READLINE_POINT}
  READLINE_LINE=$prefix$keys$suffix
  READLINE_POINT=$(( READLINE_POINT + ${#keys} ))
}

_humansh_call() {
  local original_line=$READLINE_LINE original_point=$READLINE_POINT
  local kind output error_output error_file output_file state_file state exit_status
  kind=$(_humansh_first_token_kind)
  error_file=$(command mktemp "${TMPDIR:-/tmp}/humansh-readline.XXXXXXXXXX" 2>/dev/null)
  output_file=$(command mktemp "${TMPDIR:-/tmp}/humansh-readline.XXXXXXXXXX" 2>/dev/null)
  state_file=$(command mktemp "${TMPDIR:-/tmp}/humansh-readline.XXXXXXXXXX" 2>/dev/null)
  if [[ -z $error_file || -z $output_file || -z $state_file ]] || ! command chmod 600 "$error_file" "$output_file" "$state_file" 2>/dev/null; then
    [[ -n $error_file ]] && command rm -f -- "$error_file" 2>/dev/null
    [[ -n $output_file ]] && command rm -f -- "$output_file" 2>/dev/null
    [[ -n $state_file ]] && command rm -f -- "$state_file" 2>/dev/null
    _humansh_message 'humansh could not create a private error channel; your text is unchanged.'
    return 70
  fi
  _HUMANSH_CANCELLED_BY_CLEAR=0
  _HUMANSH_DEFERRED_KEYS=''
  if [[ -t 2 && ${TERM-} != dumb ]]; then
    if ( _humansh_run_translation_with_spinner "$kind" "$output_file" "$error_file" "$state_file" ); then exit_status=0; else exit_status=$?; fi
    output=$(<"$output_file")
    state=$(<"$state_file")
    if [[ $state == C ]]; then
      _HUMANSH_CANCELLED_BY_CLEAR=1
    elif [[ $state == D* ]]; then
      _HUMANSH_DEFERRED_KEYS=${state:1}
    fi
  else
    _humansh_message "… Translating with ${HUMANSH_PROVIDER_LABEL}…"
    output=$(printf '%s' "$READLINE_LINE" | command humansh translate --protocol readline-v1 --shell bash --first-token-kind "$kind" 2>"$error_file")
    exit_status=$?
  fi
  error_output=$(<"$error_file")
  command rm -f -- "$error_file" "$output_file" "$state_file" 2>/dev/null
  case $exit_status in
    130)
      if (( _HUMANSH_CANCELLED_BY_CLEAR )); then
        _HUMANSH_CANCELLED_BY_CLEAR=0
        _HUMANSH_DEFERRED_KEYS=''
        _humansh_clear_line
      else
        READLINE_LINE=$original_line
        READLINE_POINT=$original_point
        _humansh_message 'Translation cancelled; your original text is restored.'
      fi
      return 130 ;;
    10|13|14)
      if [[ -z $output || $output == *$'\n'* || $output == *$'\r'* || $output == *$'\e'* ]]; then
        READLINE_LINE=$original_line
        READLINE_POINT=$original_point
        _humansh_message 'humansh returned an invalid generated command; your text is unchanged.'
        return 25
      fi
      READLINE_LINE=$output
      READLINE_POINT=${#READLINE_LINE}
      _HUMANSH_PENDING_BUFFER=$READLINE_LINE
      _HUMANSH_PENDING_RISK=$exit_status
      case $exit_status in
        10) _humansh_message "Generated by ${HUMANSH_PROVIDER_LABEL}. Review it, then press Enter to run." ;;
        13) _humansh_message 'Generated command changes state. Review carefully, then press Enter to run.' ;;
        14)
          _humansh_enable_pending_gate
          _humansh_message "High-risk generated command. Review it first. Next: press ${_HUMANSH_FORCE_LITERAL_LABEL} to run it." ;;
      esac
      ;;
    126|127)
      READLINE_LINE=$original_line
      READLINE_POINT=$original_point
      if [[ $_HUMANSH_WARNED_MISSING == 0 ]]; then
        _humansh_message 'humansh is unavailable; your text is unchanged.'
        _HUMANSH_WARNED_MISSING=1
      fi
      return 1 ;;
    *)
      READLINE_LINE=$original_line
      READLINE_POINT=$original_point
      _humansh_message "${error_output:-humansh could not process this line; your text is unchanged.}"
      return "$exit_status" ;;
  esac
}

_humansh_force_translate() {
  [[ -n ${READLINE_LINE//[[:space:]]/} ]] || return 0
  _humansh_call
  local exit_status=$?
  _humansh_apply_deferred_keys
  (( exit_status == 130 )) && return 0
  return "$exit_status"
}

_humansh_blocked_enter() {
  if [[ $READLINE_LINE == "$_HUMANSH_PENDING_BUFFER" ]]; then
    _humansh_message "High-risk generated command. Review it first. Next: press ${_HUMANSH_FORCE_LITERAL_LABEL} to run it."
    return 0
  fi
  printf '%s' "$READLINE_LINE" | command humansh analyze --protocol readline-v1 --shell bash >/dev/null 2>&1
  local risk_status=$?
  case $risk_status in
    10|13)
      _HUMANSH_PENDING_BUFFER=''
      _HUMANSH_PENDING_RISK=''
      _humansh_disable_pending_gate
      _humansh_message 'Edited command is no longer high risk. Review it, then press Enter again to run.' ;;
    14)
      _HUMANSH_PENDING_BUFFER=$READLINE_LINE
      _humansh_message "Edited generated command is still high risk. Next: press ${_HUMANSH_FORCE_LITERAL_LABEL} to run it." ;;
    *) _humansh_message "Could not validate the edited command. Next: press ${_HUMANSH_FORCE_LITERAL_LABEL} to run it unchanged." ;;
  esac
}

_humansh_clear_line() {
  READLINE_LINE=''
  READLINE_POINT=0
  READLINE_MARK=0
  _HUMANSH_PENDING_BUFFER=''
  _HUMANSH_PENDING_RISK=''
  _humansh_disable_pending_gate
}

humansh-on() {
  [[ $_HUMANSH_ACTIVE == 0 ]] || return 0
  local i keymap
  for ((i = 0; i < ${#_HUMANSH_KEYMAPS[@]}; i++)); do
    keymap=${_HUMANSH_KEYMAPS[i]}
    _HUMANSH_PRIOR_CLEAR[i]=$(_humansh_capture_binding "$keymap" "$_HUMANSH_CLEAR_SEQUENCE")
    _HUMANSH_PRIOR_TRANSLATE[i]=$(_humansh_capture_binding "$keymap" "$_HUMANSH_TRANSLATE_SEQUENCE")
    _HUMANSH_PRIOR_LITERAL[i]=$(_humansh_capture_binding "$keymap" "$_HUMANSH_LITERAL_SEQUENCE")
    _HUMANSH_PRIOR_ENTER_CR[i]=$(_humansh_capture_binding "$keymap" '\C-m')
    _HUMANSH_PRIOR_ENTER_LF[i]=$(_humansh_capture_binding "$keymap" '\C-j')
    _humansh_bind_shell "$keymap" "$_HUMANSH_CLEAR_SEQUENCE" _humansh_clear_line
    _humansh_bind_shell "$keymap" "$_HUMANSH_TRANSLATE_SEQUENCE" _humansh_force_translate
    bind -m "$keymap" "\"$_HUMANSH_LITERAL_SEQUENCE\": accept-line"
  done
  if [[ $_HUMANSH_PRIOR_PROMPT_COMMAND_IS_ARRAY == 1 ]]; then
    PROMPT_COMMAND=("_humansh_prompt_reset" "${_HUMANSH_PRIOR_PROMPT_COMMAND_ARRAY[@]}")
  elif [[ -n $_HUMANSH_PRIOR_PROMPT_COMMAND ]]; then
    PROMPT_COMMAND="_humansh_prompt_reset; $_HUMANSH_PRIOR_PROMPT_COMMAND"
  else
    PROMPT_COMMAND=_humansh_prompt_reset
  fi
  _HUMANSH_ACTIVE=1
}

humansh-off() {
  if [[ $_HUMANSH_ENTER_GATED == 1 && $_HUMANSH_PENDING_RISK == 14 ]]; then
    _humansh_message "humansh cannot be disabled while a high-risk generated command is pending. Next: press ${_HUMANSH_FORCE_LITERAL_LABEL} to run it, or ${HUMANSH_CLEAR_LINE_BINDING} to clear it."
    return 1
  fi
  local i keymap
  _humansh_disable_pending_gate
  for ((i = 0; i < ${#_HUMANSH_KEYMAPS[@]}; i++)); do
    keymap=${_HUMANSH_KEYMAPS[i]}
    _humansh_restore_binding "$keymap" "$_HUMANSH_CLEAR_SEQUENCE" "${_HUMANSH_PRIOR_CLEAR[i]}"
    _humansh_restore_binding "$keymap" "$_HUMANSH_TRANSLATE_SEQUENCE" "${_HUMANSH_PRIOR_TRANSLATE[i]}"
    _humansh_restore_binding "$keymap" "$_HUMANSH_LITERAL_SEQUENCE" "${_HUMANSH_PRIOR_LITERAL[i]}"
  done
  if [[ $_HUMANSH_PRIOR_PROMPT_COMMAND_IS_ARRAY == 1 ]]; then
    PROMPT_COMMAND=("${_HUMANSH_PRIOR_PROMPT_COMMAND_ARRAY[@]}")
  elif [[ -z $_HUMANSH_PRIOR_PROMPT_COMMAND_WAS_SET ]]; then
    unset PROMPT_COMMAND
  else
    PROMPT_COMMAND=$_HUMANSH_PRIOR_PROMPT_COMMAND
  fi
  _HUMANSH_PENDING_BUFFER=''
  _HUMANSH_PENDING_RISK=''
  _HUMANSH_ACTIVE=0
}

humansh-toggle() { if [[ $_HUMANSH_ACTIVE == 1 ]]; then humansh-off; else humansh-on; fi; }

humansh-bindings() {
  local keymap
  for keymap in "${_HUMANSH_KEYMAPS[@]}"; do
    printf '%s: clear-line=%s, force-translate=%s, force-literal=%s\n' "$keymap" \
      "$(_humansh_capture_binding "$keymap" "$_HUMANSH_CLEAR_SEQUENCE")" \
      "$(_humansh_capture_binding "$keymap" "$_HUMANSH_TRANSLATE_SEQUENCE")" \
      "$(_humansh_capture_binding "$keymap" "$_HUMANSH_LITERAL_SEQUENCE")"
  done
}

_HUMANSH_BASH_LOADED=1
humansh-on
if [[ -t 2 && ${TERM-} != dumb ]]; then
  printf '\nhumansh active in Bash — type natural language and press %s; Enter runs normal Bash commands.\n' "$_HUMANSH_FORCE_TRANSLATE_LABEL" >&2
fi
