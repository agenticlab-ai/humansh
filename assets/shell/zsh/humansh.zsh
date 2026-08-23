# Embedded humansh ZLE integration. Installed and configured by `humansh setup`.
# shellcheck shell=bash disable=SC2034,SC2206,SC2296

[[ -o interactive ]] || return 0
zmodload -i zsh/zle 2>/dev/null || { print -ru2 -- 'humansh disabled: ZLE is unavailable in this interactive Zsh.'; return 0; }
[[ ${HUMANSH_DISABLE-0} == 1 ]] && return 0

: "${HUMANSH_SMART_ENTER:=1}"
: "${HUMANSH_CLEAR_LINE_BINDING:=^[}"
: "${HUMANSH_FORCE_TRANSLATE_BINDING:=^G}"
: "${HUMANSH_FORCE_LITERAL_BINDING:=^X^M}"
: "${HUMANSH_PROVIDER_LABEL:=provider}"

_humansh_activation_error() {
  local message=$1
  if [[ -n ${WIDGET-} ]]; then
    zle -M -- "$message"
  else
    print -ru2 -- "$message"
  fi
}

_humansh_binding_valid() {
  local value=$1 character
  local -i index
  (( ${#value} >= 1 && ${#value} <= 32 )) || return 1
  for (( index = 1; index <= ${#value}; index++ )); do
    character=${value[index]}
    case $character in
      [A-Za-z0-9]|'^'|'['|']'|'_'|'+'|'.'|','|':'|'/'|'='|'-') ;;
      *) return 1 ;;
    esac
  done
}

if [[ $HUMANSH_SMART_ENTER != 0 && $HUMANSH_SMART_ENTER != 1 ]]; then
  _humansh_activation_error 'humansh disabled: HUMANSH_SMART_ENTER must be 0 or 1. Next: run humansh doctor --fix.'
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

_humansh_binding_label() {
  local binding=$1 label='' key control
  local -i index=1
  while (( index <= ${#binding} )); do
    key=${binding[index]}
    if [[ $key == '^' ]] && (( index < ${#binding} )); then
      (( index++ ))
      control=${(U)binding[index]}
      case $control in
        M) key='Enter' ;;
        I) key='Tab' ;;
        '[') key='Esc' ;;
        *) key="Ctrl-${control}" ;;
      esac
    fi
    [[ -n $label ]] && label+=' then '
    label+=$key
    (( index++ ))
  done
  print -r -- "$label"
}

# Encode the validated portable binding notation as comma-delimited byte
# values. The spinner reads terminal input directly while a ZLE widget is
# active, so it cannot rely on bindkey dispatch until the widget returns.
_humansh_binding_codes() {
	local binding=$1 result='' key control
	local -i index=1 code
	while (( index <= ${#binding} )); do
		key=${binding[index]}
		if [[ $key == '^' ]] && (( index < ${#binding} )); then
			(( index++ ))
			control=${(U)binding[index]}
			if [[ $control == '[' ]]; then
				code=27
			else
				printf -v code '%d' "'$control"
				(( code &= 31 ))
			fi
		else
			printf -v code '%d' "'$key"
		fi
		result+="${code},"
		(( index++ ))
	done
	print -r -- "$result"
}

typeset -g _HUMANSH_PENDING_BUFFER="${_HUMANSH_PENDING_BUFFER-}"
typeset -g _HUMANSH_PENDING_RISK="${_HUMANSH_PENDING_RISK-}"
typeset -g _HUMANSH_WARNED_MISSING=0
typeset -g _HUMANSH_PROVIDER_LABEL=${HUMANSH_PROVIDER_LABEL//[[:cntrl:]]/}
typeset -g _HUMANSH_FORCE_TRANSLATE_LABEL
typeset -g _HUMANSH_FORCE_LITERAL_LABEL
typeset -g _HUMANSH_CLEAR_CODES
_HUMANSH_FORCE_TRANSLATE_LABEL=$(_humansh_binding_label "$HUMANSH_FORCE_TRANSLATE_BINDING")
_HUMANSH_FORCE_LITERAL_LABEL=$(_humansh_binding_label "$HUMANSH_FORCE_LITERAL_BINDING")
_HUMANSH_CLEAR_CODES=$(_humansh_binding_codes "$HUMANSH_CLEAR_LINE_BINDING")
typeset -gi _HUMANSH_TEMPORARY_ENTER_GATE=${_HUMANSH_TEMPORARY_ENTER_GATE:-0}
typeset -gi _HUMANSH_MESSAGE_ACTIVE=0
typeset -gi _HUMANSH_CANCELLED_BY_CLEAR=0
typeset -g _HUMANSH_DEFERRED_KEYS=''
typeset -gA _HUMANSH_PRIOR_ENTER_WIDGETS
typeset -gA _HUMANSH_PRIOR_LINEFEED_WIDGETS
typeset -gA _HUMANSH_PRIOR_CLEAR_WIDGETS
typeset -gA _HUMANSH_PRIOR_TRANSLATE_WIDGETS
typeset -gA _HUMANSH_PRIOR_LITERAL_WIDGETS

# HUMANSH_PROVIDER_LABEL above is the baseline, written into the managed block by
# setup. Nothing is resolved by spawning humansh here: doing that while .zshrc is
# sourced would add an untimed blocking subprocess to every interactive shell
# start, so a stalled home directory would hang every new terminal. On the smart
# path the live label rides back on the `classify --zle-status` call the widget
# already makes, so it stays correct even before a shell picks up a new block.

_humansh_message() {
  local message=${1//[[:cntrl:]]/ }
  _HUMANSH_MESSAGE_ACTIVE=1
  zle -M -- "${message[1,500]}"
}

_humansh_first_token_kind() {
  local -a words
  words=(${(z)BUFFER})
  (( ${#words} )) || { print -r -- empty; return; }
  local token=${words[1]} result
  result=$(whence -w -- "$token" 2>/dev/null) || { print -r -- unresolved; return; }
  case $result in
    *': alias') print -r -- alias ;;
    *': function') print -r -- function ;;
    *': builtin') print -r -- builtin ;;
    *': reserved') print -r -- reserved ;;
    *': command'|*': hashed') print -r -- command ;;
    *) print -r -- unknown ;;
  esac
}

_humansh_prior_enter() {
  local keymap=${KEYMAP:-main}
	local widget
	_HUMANSH_MESSAGE_ACTIVE=0
	if [[ ${KEYS-} == $'\n' ]]; then
		widget=${_HUMANSH_PRIOR_LINEFEED_WIDGETS[$keymap]:-${_HUMANSH_PRIOR_LINEFEED_WIDGETS[main]:-accept-line}}
	else
		widget=${_HUMANSH_PRIOR_ENTER_WIDGETS[$keymap]:-${_HUMANSH_PRIOR_ENTER_WIDGETS[main]:-accept-line}}
	fi
  zle "$widget"
}

_humansh_enable_pending_gate() {
  [[ $HUMANSH_SMART_ENTER == 0 && $_HUMANSH_TEMPORARY_ENTER_GATE == 0 ]] || return
  local keymap
  for keymap in main emacs viins vicmd; do
    bindkey -M "$keymap" '^M' _humansh_smart_enter
		bindkey -M "$keymap" '^J' _humansh_smart_enter
  done
  _HUMANSH_TEMPORARY_ENTER_GATE=1
}

_humansh_disable_pending_gate() {
  [[ $_HUMANSH_TEMPORARY_ENTER_GATE == 1 ]] || return
  local keymap
  for keymap in main emacs viins vicmd; do
    [[ -n ${_HUMANSH_PRIOR_ENTER_WIDGETS[$keymap]-} ]] && bindkey -M "$keymap" '^M' "${_HUMANSH_PRIOR_ENTER_WIDGETS[$keymap]}"
		[[ -n ${_HUMANSH_PRIOR_LINEFEED_WIDGETS[$keymap]-} ]] && bindkey -M "$keymap" '^J' "${_HUMANSH_PRIOR_LINEFEED_WIDGETS[$keymap]}"
  done
  _HUMANSH_TEMPORARY_ENTER_GATE=0
}

_humansh_run_translation_with_spinner() {
	local mode=$1 kind=$2 output_file=$3 error_file=$4 provider_pid key candidate_keys='' candidate_codes=''
	local -a frames=(⠋ ⠙ ⠹ ⠸ ⠼ ⠴ ⠦ ⠧ ⠇ ⠏)
	local -i frame_index=1 interrupted=0 exit_status key_code
	setopt localoptions localtraps nomonitor
	trap 'interrupted=1; [[ -n $provider_pid ]] && kill -TERM -- "$provider_pid" 2>/dev/null' INT
	_HUMANSH_CANCELLED_BY_CLEAR=0
	_HUMANSH_DEFERRED_KEYS=''

	print -rn -- "$BUFFER" | command humansh "$mode" --protocol zle-v1 --shell zsh --first-token-kind "$kind" >"$output_file" 2>"$error_file" &
	provider_pid=$!
	while kill -0 -- "$provider_pid" 2>/dev/null; do
		_humansh_message "${frames[frame_index]} Translating with ${_HUMANSH_PROVIDER_LABEL}…"
		zle -R
		key=''
		if read -r -k 1 -t 0.08 key 2>/dev/null </dev/tty; then
			printf -v key_code '%d' "'$key"
			candidate_keys+=$key
			candidate_codes+="${key_code},"
			if [[ $candidate_codes == "$_HUMANSH_CLEAR_CODES" ]]; then
				_HUMANSH_CANCELLED_BY_CLEAR=1
				kill -TERM -- "$provider_pid" 2>/dev/null
				break
			fi
			if (( key_code == 3 )); then
				interrupted=1
				kill -TERM -- "$provider_pid" 2>/dev/null
				break
			fi
			if [[ $_HUMANSH_CLEAR_CODES != "$candidate_codes"* ]]; then
				[[ $candidate_keys != *[[:cntrl:]]* ]] && _HUMANSH_DEFERRED_KEYS+=$candidate_keys
				candidate_keys=''
				candidate_codes=''
			fi
		fi
		(( frame_index = frame_index == ${#frames} ? 1 : frame_index + 1 ))
	done
	[[ -n $candidate_keys && $candidate_keys != *[[:cntrl:]]* ]] && _HUMANSH_DEFERRED_KEYS+=$candidate_keys
	if wait "$provider_pid"; then exit_status=0; else exit_status=$?; fi
	trap - INT
	(( _HUMANSH_CANCELLED_BY_CLEAR )) && return 130
	(( interrupted )) && return 130
	return $exit_status
}

_humansh_replay_deferred_keys() {
	local keys=$_HUMANSH_DEFERRED_KEYS
	_HUMANSH_DEFERRED_KEYS=''
	[[ -n $keys ]] && zle -U "$keys"
}

_humansh_call() {
	local mode=$1 original_buffer=$BUFFER original_cursor=$CURSOR kind output error_output error_file output_file exit_status zle_status provider_label
	setopt localtraps
	kind=$(_humansh_first_token_kind)
	if [[ $mode == translate ]]; then
		zle_status=translate
	else
		zle_status=$(print -rn -- "$BUFFER" | command humansh classify --zle-status --shell zsh --first-token-kind "$kind" 2>/dev/null)
	fi
	# The hint is either empty or "translate", optionally followed by the provider
	# label. The label comes from a fixed enum in Go, but strip control characters
	# anyway before it reaches the display.
	if [[ $zle_status == 'translate '* ]]; then
		provider_label=${zle_status#translate }
		_HUMANSH_PROVIDER_LABEL=${provider_label//[[:cntrl:]]/}
		zle_status=translate
	fi
	if [[ $zle_status == translate ]]; then
		output_file=$(command mktemp "${TMPDIR:-/tmp}/humansh-zle.XXXXXXXXXX" 2>/dev/null)
	fi
	error_file=$(command mktemp "${TMPDIR:-/tmp}/humansh-zle.XXXXXXXXXX" 2>/dev/null)
	if [[ -z $error_file || ( $zle_status == translate && -z $output_file ) ]] ||
	   ! command chmod 600 "$error_file" ${output_file:+"$output_file"} 2>/dev/null; then
		[[ -n $error_file ]] && command rm -f -- "$error_file" 2>/dev/null
		[[ -n $output_file ]] && command rm -f -- "$output_file" 2>/dev/null
		BUFFER=$original_buffer
		CURSOR=$original_cursor
		_humansh_message "humansh could not create a private result channel; your text is unchanged. Next: press ${_HUMANSH_FORCE_LITERAL_LABEL} to run it unchanged."
		return 70
	fi
	if [[ $zle_status == translate && ${TERM-} != dumb ]]; then
		_humansh_run_translation_with_spinner "$mode" "$kind" "$output_file" "$error_file"
		exit_status=$?
		output=$(<"$output_file")
	else
		if [[ $zle_status == translate ]]; then
			_humansh_message "… Translating with ${_HUMANSH_PROVIDER_LABEL}…"
			zle -R
		fi
		output=$(print -rn -- "$BUFFER" | command humansh "$mode" --protocol zle-v1 --shell zsh --first-token-kind "$kind" 2>"$error_file")
		exit_status=$?
	fi
	error_output=$(<"$error_file")
	command rm -f -- "$error_file" ${output_file:+"$output_file"} 2>/dev/null
	if (( exit_status == 130 )); then
		if (( _HUMANSH_CANCELLED_BY_CLEAR )); then
			_HUMANSH_CANCELLED_BY_CLEAR=0
			_HUMANSH_DEFERRED_KEYS=''
			_humansh_clear_line
			return 130
		fi
    BUFFER=$original_buffer
    CURSOR=$original_cursor
    _humansh_message "Translation cancelled; your original text is restored. Next: edit it, or press ${_HUMANSH_FORCE_TRANSLATE_LABEL} to try again."
    return 130
	fi
	case $exit_status in
		0)
			if [[ -n $output || -n $error_output ]]; then
				BUFFER=$original_buffer
				CURSOR=$original_cursor
				_humansh_message "humansh returned an invalid literal result; your text is unchanged. Next: press ${_HUMANSH_FORCE_LITERAL_LABEL} to run it unchanged."
				return 70
			fi
			return 0 ;;
		10|13|14)
			[[ -n $output && $output != *$'\n'* && $output != *$'\r'* && $output != *$'\0'* ]] || { BUFFER=$original_buffer; CURSOR=$original_cursor; _humansh_message "humansh returned an invalid generated command; your text is unchanged. Next: edit it and press Enter, or press ${_HUMANSH_FORCE_LITERAL_LABEL} to run it unchanged."; return 25; }
      BUFFER=$output
      CURSOR=${#BUFFER}
      _HUMANSH_PENDING_BUFFER=$BUFFER
      _HUMANSH_PENDING_RISK=$exit_status
			_humansh_enable_pending_gate
			case $exit_status in
				10) _humansh_message "Generated by ${_HUMANSH_PROVIDER_LABEL}. Review it, then press Enter to run." ;;
        13) _humansh_message 'Generated command changes state. Review carefully, then press Enter to run.' ;;
        14) _humansh_message "High-risk generated command. Review it first. Next: press ${_HUMANSH_FORCE_LITERAL_LABEL} to run it." ;;
      esac
			return 0 ;;
		11|12|15|20|21|22|23|24|25|26|70)
			BUFFER=$original_buffer
			CURSOR=$original_cursor
			if [[ -n $output ]]; then
				_humansh_message "humansh returned an invalid result; your text is unchanged. Next: press ${_HUMANSH_FORCE_LITERAL_LABEL} to run it unchanged."
				return 70
			fi
			case $exit_status in
				11) _humansh_message "Not sure whether this is English or a command. Next: press ${_HUMANSH_FORCE_TRANSLATE_LABEL} to translate it, or press ${_HUMANSH_FORCE_LITERAL_LABEL} to run it unchanged." ;;
				15) _humansh_message "This request cannot be represented as one shell command. Next: edit it and press Enter, or press ${_HUMANSH_FORCE_LITERAL_LABEL} to run it unchanged." ;;
				*) _humansh_message "${error_output:-humansh could not process this line. Edit it, then press Enter to try again.}" ;;
			esac
      return $exit_status ;;
    126|127)
      BUFFER=$original_buffer
      CURSOR=$original_cursor
      if [[ $_HUMANSH_WARNED_MISSING == 0 ]]; then
        _humansh_message 'humansh is unavailable; using the previous Enter binding.'
		zle -R
        _HUMANSH_WARNED_MISSING=1
      fi
      _humansh_prior_enter
      return 1 ;;
    *)
      BUFFER=$original_buffer
      CURSOR=$original_cursor
      _humansh_message "humansh returned an unknown result; your text is unchanged. Next: press ${_HUMANSH_FORCE_LITERAL_LABEL} to run it unchanged."
      return $exit_status ;;
  esac
}

_humansh_smart_enter() {
  [[ -n ${BUFFER//[[:space:]]/} ]] || { _humansh_prior_enter; return; }
  if [[ -n $_HUMANSH_PENDING_BUFFER && $BUFFER == "$_HUMANSH_PENDING_BUFFER" ]]; then
    if [[ $_HUMANSH_PENDING_RISK == 14 ]]; then
      _humansh_message "High-risk generated command. Review it first. Next: press ${_HUMANSH_FORCE_LITERAL_LABEL} to run it."
      return
    fi
    _HUMANSH_PENDING_BUFFER=''
    _HUMANSH_PENDING_RISK=''
		_humansh_disable_pending_gate
    _humansh_prior_enter
    return
  fi
  if [[ -n $_HUMANSH_PENDING_BUFFER && $BUFFER != "$_HUMANSH_PENDING_BUFFER" ]]; then
    local risk_status
    print -rn -- "$BUFFER" | command humansh analyze --protocol zle-v1 >/dev/null 2>&1
    risk_status=$?
    if [[ $risk_status == 14 ]]; then
      _HUMANSH_PENDING_BUFFER=$BUFFER
      _HUMANSH_PENDING_RISK=14
      _humansh_message "Edited generated command is still high risk. Review it first. Next: press ${_HUMANSH_FORCE_LITERAL_LABEL} to run it."
      return
    fi
    if [[ $risk_status != 10 && $risk_status != 13 ]]; then
      _humansh_message "Could not validate the edited generated command; your edit is preserved. Next: review it, then press ${_HUMANSH_FORCE_LITERAL_LABEL} to run it."
      return
    fi
    _HUMANSH_PENDING_BUFFER=''
    _HUMANSH_PENDING_RISK=''
		_humansh_disable_pending_gate
  fi
  _humansh_call smart
  local exit_status=$?
  [[ $exit_status == 0 && -z $_HUMANSH_PENDING_BUFFER ]] && _humansh_prior_enter
	_humansh_replay_deferred_keys
}

_humansh_force_translate() {
	_humansh_call translate
	local exit_status=$?
	_humansh_replay_deferred_keys
	return $exit_status
}
_humansh_clear_line() {
  # Escape is a cancel action for live, unsubmitted state—not a terminal or
  # history eraser. In particular, do not ask ZLE to redraw/clear a message
  # when the prompt is already empty: repeated Escape presses must be no-ops.
  [[ -n $BUFFER || -n $_HUMANSH_PENDING_BUFFER || $_HUMANSH_MESSAGE_ACTIVE == 1 ]] || return 0
  local -i had_message=$_HUMANSH_MESSAGE_ACTIVE
  BUFFER=''
  CURSOR=0
  MARK=0
  REGION_ACTIVE=0
  _HUMANSH_PENDING_BUFFER=''
  _HUMANSH_PENDING_RISK=''
  _humansh_disable_pending_gate
  _HUMANSH_MESSAGE_ACTIVE=0
  (( had_message )) && zle -M ''
}
_humansh_force_literal() {
  _HUMANSH_PENDING_BUFFER=''
  _HUMANSH_PENDING_RISK=''
  _humansh_disable_pending_gate
  _humansh_prior_enter
}

zle -N _humansh_smart_enter
zle -N _humansh_clear_line
zle -N _humansh_force_translate
zle -N _humansh_force_literal

humansh-on() {
  local keymap binding widget
  for keymap in main emacs viins vicmd; do
    binding=$(bindkey -M "$keymap" '^M' 2>/dev/null)
    widget=${binding##* }
    [[ $widget == _humansh_smart_enter ]] || _HUMANSH_PRIOR_ENTER_WIDGETS[$keymap]=${widget:-accept-line}
		binding=$(bindkey -M "$keymap" '^J' 2>/dev/null)
		widget=${binding##* }
		[[ $widget == _humansh_smart_enter ]] || _HUMANSH_PRIOR_LINEFEED_WIDGETS[$keymap]=${widget:-accept-line}
		binding=$(bindkey -M "$keymap" "$HUMANSH_CLEAR_LINE_BINDING" 2>/dev/null)
		widget=${binding##* }
		[[ $widget == _humansh_clear_line ]] || _HUMANSH_PRIOR_CLEAR_WIDGETS[$keymap]=${widget:-undefined-key}
		binding=$(bindkey -M "$keymap" "$HUMANSH_FORCE_TRANSLATE_BINDING" 2>/dev/null)
		widget=${binding##* }
		[[ $widget == _humansh_force_translate ]] || _HUMANSH_PRIOR_TRANSLATE_WIDGETS[$keymap]=${widget:-undefined-key}
		binding=$(bindkey -M "$keymap" "$HUMANSH_FORCE_LITERAL_BINDING" 2>/dev/null)
		widget=${binding##* }
		[[ $widget == _humansh_force_literal ]] || _HUMANSH_PRIOR_LITERAL_WIDGETS[$keymap]=${widget:-undefined-key}
  done
  for keymap in main emacs viins vicmd; do
		if [[ $HUMANSH_SMART_ENTER == 1 ]]; then
			bindkey -M "$keymap" '^M' _humansh_smart_enter
			bindkey -M "$keymap" '^J' _humansh_smart_enter
		fi
    bindkey -M "$keymap" "$HUMANSH_CLEAR_LINE_BINDING" _humansh_clear_line
    bindkey -M "$keymap" "$HUMANSH_FORCE_TRANSLATE_BINDING" _humansh_force_translate
    bindkey -M "$keymap" "$HUMANSH_FORCE_LITERAL_BINDING" _humansh_force_literal
  done
	[[ -n $_HUMANSH_PENDING_BUFFER ]] && _humansh_enable_pending_gate
}

_humansh_restore_binding() {
	local keymap=$1 sequence=$2 widget=$3
	if [[ $widget == undefined-key ]]; then
		bindkey -r -M "$keymap" "$sequence" 2>/dev/null
	else
		bindkey -M "$keymap" "$sequence" "$widget"
	fi
}

humansh-off() {
	if [[ -n $_HUMANSH_PENDING_BUFFER && $_HUMANSH_PENDING_RISK == 14 ]]; then
		_humansh_activation_error "humansh cannot be disabled while a high-risk generated command is pending. Next: review it and press ${_HUMANSH_FORCE_LITERAL_LABEL} to run it, or edit it into a lower-risk command."
		return 1
	fi
  local keymap
  for keymap in main emacs viins vicmd; do
		[[ -n ${_HUMANSH_PRIOR_ENTER_WIDGETS[$keymap]-} ]] && bindkey -M "$keymap" '^M' "${_HUMANSH_PRIOR_ENTER_WIDGETS[$keymap]}"
		[[ -n ${_HUMANSH_PRIOR_LINEFEED_WIDGETS[$keymap]-} ]] && bindkey -M "$keymap" '^J' "${_HUMANSH_PRIOR_LINEFEED_WIDGETS[$keymap]}"
		[[ -n ${_HUMANSH_PRIOR_CLEAR_WIDGETS[$keymap]-} ]] && _humansh_restore_binding "$keymap" "$HUMANSH_CLEAR_LINE_BINDING" "${_HUMANSH_PRIOR_CLEAR_WIDGETS[$keymap]}"
		[[ -n ${_HUMANSH_PRIOR_TRANSLATE_WIDGETS[$keymap]-} ]] && _humansh_restore_binding "$keymap" "$HUMANSH_FORCE_TRANSLATE_BINDING" "${_HUMANSH_PRIOR_TRANSLATE_WIDGETS[$keymap]}"
		[[ -n ${_HUMANSH_PRIOR_LITERAL_WIDGETS[$keymap]-} ]] && _humansh_restore_binding "$keymap" "$HUMANSH_FORCE_LITERAL_BINDING" "${_HUMANSH_PRIOR_LITERAL_WIDGETS[$keymap]}"
  done
	_HUMANSH_TEMPORARY_ENTER_GATE=0
	_HUMANSH_PENDING_BUFFER=''
	_HUMANSH_PENDING_RISK=''
	_HUMANSH_MESSAGE_ACTIVE=0
}

humansh-toggle() {
  if [[ $(bindkey -M main '^M') == *'_humansh_smart_enter' ]]; then humansh-off; else humansh-on; fi
}

humansh-bindings() {
	local keymap enter_widget linefeed_widget clear_widget translate_widget literal_widget
	for keymap in main emacs viins vicmd; do
		enter_widget=$(bindkey -M "$keymap" '^M' 2>/dev/null); enter_widget=${enter_widget##* }
		linefeed_widget=$(bindkey -M "$keymap" '^J' 2>/dev/null); linefeed_widget=${linefeed_widget##* }
		clear_widget=$(bindkey -M "$keymap" "$HUMANSH_CLEAR_LINE_BINDING" 2>/dev/null); clear_widget=${clear_widget##* }
		translate_widget=$(bindkey -M "$keymap" "$HUMANSH_FORCE_TRANSLATE_BINDING" 2>/dev/null); translate_widget=${translate_widget##* }
		literal_widget=$(bindkey -M "$keymap" "$HUMANSH_FORCE_LITERAL_BINDING" 2>/dev/null); literal_widget=${literal_widget##* }
		print -r -- "$keymap: ^M=$enter_widget (previous=${_HUMANSH_PRIOR_ENTER_WIDGETS[$keymap]:-unknown}), ^J=$linefeed_widget (previous=${_HUMANSH_PRIOR_LINEFEED_WIDGETS[$keymap]:-unknown}), clear-line=$clear_widget (previous=${_HUMANSH_PRIOR_CLEAR_WIDGETS[$keymap]:-unknown}), force-translate=$translate_widget (previous=${_HUMANSH_PRIOR_TRANSLATE_WIDGETS[$keymap]:-unknown}), force-literal=$literal_widget (previous=${_HUMANSH_PRIOR_LITERAL_WIDGETS[$keymap]:-unknown})"
	done
}

humansh-on
