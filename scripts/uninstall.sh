#!/bin/sh
set -eu

purge=0
case $# in
  0) ;;
  1) [ "$1" = "--purge" ] || { echo "usage: scripts/uninstall.sh [--purge]" >&2; exit 2; }; purge=1 ;;
  *) echo "usage: scripts/uninstall.sh [--purge]" >&2; exit 2 ;;
esac
if [ "$purge" -eq 1 ]; then
  printf 'Purge humansh configuration and credentials? [y/N] '
  IFS= read -r answer || answer=
  case $answer in
    y|Y|yes|YES) ;;
    *) echo "Uninstall cancelled. Nothing was changed."; exit 0 ;;
  esac
fi
data_dir=${XDG_DATA_HOME:-"$HOME/.local/share"}/humansh
state_file="$data_dir/install-state.toml"
binary="$HOME/.local/bin/humansh"
zsh_startup="$HOME/.zshrc"
zsh_asset="$data_dir/shell/zsh/humansh.zsh"
bash_startup="$HOME/.bashrc"
bash_asset="$data_dir/shell/bash/humansh.bash"
use_zsh=1
use_bash=1

state_string() {
  awk -v wanted="$1" '
    $1 == wanted && $2 == "=" {
      line = $0
      sub(/^[^=]*=[[:space:]]*/, "", line)
      if (line !~ /^".*"$/ || index(line, "\\") != 0 || found) exit 2
      print substr(line, 2, length(line) - 2)
      found = 1
    }
    END { if (!found) exit 1 }
  ' "$state_file"
}

state_integer() {
  awk -v wanted="$1" '
    $1 == wanted {
      if ($2 != "=" || $3 !~ /^[0-9]+$/ || NF != 3 || found) exit 2
      print $3
      found = 1
    }
    END { if (!found) exit 1 }
  ' "$state_file"
}

file_mode() {
  if mode_output=$(stat -f '%Lp' "$1" 2>/dev/null); then
    :
  elif mode_output=$(stat -c '%a' "$1" 2>/dev/null); then
    :
  else
    return 1
  fi
  case $mode_output in
    ''|*[!0-7]*) return 1 ;;
  esac
  printf '%s\n' "$mode_output"
}

if [ -L "$state_file" ]; then
  echo "humansh uninstall: install state must be a regular file, not a symlink; no files were changed." >&2
  exit 1
fi
if [ -f "$state_file" ]; then
  state_mode=$(file_mode "$state_file") || { echo "humansh uninstall: could not determine install state permissions; no files were changed." >&2; exit 1; }
  [ "$state_mode" = 600 ] || { echo "humansh uninstall: install state has unsafe mode $state_mode; run 'chmod 600 $state_file' and retry." >&2; exit 1; }
  state_version=$(state_integer version) || { echo "humansh uninstall: install state is invalid; run 'humansh doctor --fix' and retry." >&2; exit 1; }
  block_version=$(state_integer managed_block_version) || { echo "humansh uninstall: install state is invalid; run 'humansh doctor --fix' and retry." >&2; exit 1; }
  state_binary=$(state_string binary_path) || { echo "humansh uninstall: install state binary_path is invalid; run 'humansh doctor --fix' and retry." >&2; exit 1; }
  state_installed_version=$(state_string installed_version) || { echo "humansh uninstall: install state installed_version is invalid; run 'humansh doctor --fix' and retry." >&2; exit 1; }
  if [ -z "$state_installed_version" ] || [ "$block_version" != 1 ]; then
    echo "humansh uninstall: install state version is unsupported." >&2
    exit 1
  fi
  [ "$state_binary" = "$binary" ] || { echo "humansh uninstall: install-state paths do not match the current humansh layout; no files were changed." >&2; exit 1; }
  binary=$state_binary
  case $state_version in
    1)
      state_asset=$(state_string shell_asset_path) || { echo "humansh uninstall: install state shell_asset_path is invalid; run 'humansh doctor --fix' and retry." >&2; exit 1; }
      state_startup=$(state_string startup_file) || { echo "humansh uninstall: install state startup_file is invalid; run 'humansh doctor --fix' and retry." >&2; exit 1; }
      state_shell=$(state_string shell) || { echo "humansh uninstall: install state shell is invalid; run 'humansh doctor --fix' and retry." >&2; exit 1; }
      state_protocol=$(state_string protocol) || { echo "humansh uninstall: install state protocol is invalid; run 'humansh doctor --fix' and retry." >&2; exit 1; }
      state_asset_sha256=$(state_string shell_asset_sha256) || { echo "humansh uninstall: install state shell_asset_sha256 is invalid; run 'humansh doctor --fix' and retry." >&2; exit 1; }
      case $state_asset_sha256 in *[!0-9A-Fa-f]*|'') echo "humansh uninstall: install state shell_asset_sha256 is invalid." >&2; exit 1 ;; esac
      [ "${#state_asset_sha256}" -eq 64 ] || { echo "humansh uninstall: install state digest is unsupported." >&2; exit 1; }
      case $state_shell:$state_protocol in
        zsh:zle-v1)
          use_zsh=1
          use_bash=0
          if [ "$state_asset" != "$zsh_asset" ] || [ "$state_startup" != "$zsh_startup" ]; then
            echo "humansh uninstall: install-state paths do not match the current humansh layout; no files were changed." >&2
            exit 1
          fi ;;
        bash:readline-v1)
          use_zsh=0
          use_bash=1
          if [ "$state_asset" != "$bash_asset" ] || [ "$state_startup" != "$bash_startup" ]; then
            echo "humansh uninstall: install-state paths do not match the current humansh layout; no files were changed." >&2
            exit 1
          fi ;;
        *) echo "humansh uninstall: install state shell or protocol is unsupported." >&2; exit 1 ;;
      esac
      ;;
    2)
      state_shells=$(state_string shells) || { echo "humansh uninstall: install state shells are invalid; run 'humansh doctor --fix' and retry." >&2; exit 1; }
      case $state_shells in
        zsh) use_zsh=1; use_bash=0 ;;
        bash) use_zsh=0; use_bash=1 ;;
        zsh,bash) use_zsh=1; use_bash=1 ;;
        *) echo "humansh uninstall: install state shells are unsupported or duplicated." >&2; exit 1 ;;
      esac
      if [ "$use_zsh" -eq 1 ]; then
        zsh_protocol=$(state_string zsh_protocol) || { echo "humansh uninstall: zsh install state is invalid." >&2; exit 1; }
        zsh_state_asset=$(state_string zsh_shell_asset_path) || { echo "humansh uninstall: zsh install state is invalid." >&2; exit 1; }
        zsh_digest=$(state_string zsh_shell_asset_sha256) || { echo "humansh uninstall: zsh install state is invalid." >&2; exit 1; }
        zsh_state_startup=$(state_string zsh_startup_file) || { echo "humansh uninstall: zsh install state is invalid." >&2; exit 1; }
        case $zsh_digest in *[!0-9A-Fa-f]*|'') echo "humansh uninstall: zsh install-state digest is invalid." >&2; exit 1 ;; esac
        if [ "$zsh_protocol" != zle-v1 ] || [ "${#zsh_digest}" -ne 64 ] || [ "$zsh_state_asset" != "$zsh_asset" ] || [ "$zsh_state_startup" != "$zsh_startup" ]; then
          echo "humansh uninstall: zsh install state does not match the current humansh layout." >&2
          exit 1
        fi
      fi
      if [ "$use_bash" -eq 1 ]; then
        bash_protocol=$(state_string bash_protocol) || { echo "humansh uninstall: bash install state is invalid." >&2; exit 1; }
        bash_state_asset=$(state_string bash_shell_asset_path) || { echo "humansh uninstall: bash install state is invalid." >&2; exit 1; }
        bash_digest=$(state_string bash_shell_asset_sha256) || { echo "humansh uninstall: bash install state is invalid." >&2; exit 1; }
        bash_state_startup=$(state_string bash_startup_file) || { echo "humansh uninstall: bash install state is invalid." >&2; exit 1; }
        case $bash_digest in *[!0-9A-Fa-f]*|'') echo "humansh uninstall: bash install-state digest is invalid." >&2; exit 1 ;; esac
        if [ "$bash_protocol" != readline-v1 ] || [ "${#bash_digest}" -ne 64 ] || [ "$bash_state_asset" != "$bash_asset" ] || [ "$bash_state_startup" != "$bash_startup" ]; then
          echo "humansh uninstall: bash install state does not match the current humansh layout." >&2
          exit 1
        fi
      fi
      ;;
    *) echo "humansh uninstall: install state version is unsupported." >&2; exit 1 ;;
  esac
fi

validate_managed_target() {
  managed_target=$1
  if { [ -e "$managed_target" ] || [ -L "$managed_target" ]; } && [ ! -f "$managed_target" ] && [ ! -L "$managed_target" ]; then
    echo "humansh uninstall: managed target $managed_target is not a regular file or symlink; no files were changed." >&2
    return 1
  fi
}
validate_managed_target "$binary"
[ "$use_zsh" -eq 0 ] || validate_managed_target "$zsh_asset"
[ "$use_bash" -eq 0 ] || validate_managed_target "$bash_asset"

resolve_startup() {
  candidate=$1
  if [ -e "$candidate" ] && [ ! -f "$candidate" ] && [ ! -L "$candidate" ]; then
    echo "humansh uninstall: $candidate is not a regular file or symlink; no files were changed." >&2
    return 1
  fi
  candidate_write=$candidate
  if [ -L "$candidate" ]; then
    link_target=$(readlink "$candidate") || { echo "humansh uninstall: could not resolve startup-file symlink; no files were changed." >&2; return 1; }
    case $link_target in
      /*) candidate_write=$link_target ;;
      *) candidate_write=$(dirname "$candidate")/$link_target ;;
    esac
    [ ! -L "$candidate_write" ] || { echo "humansh uninstall: chained startup-file symlinks require manual managed-block removal." >&2; return 1; }
    [ -f "$candidate_write" ] || { echo "humansh uninstall: startup-file symlink target is not a regular file; no files were changed." >&2; return 1; }
  fi
  printf '%s\n' "$candidate_write"
}

validate_startup() {
  candidate=$1
  [ ! -f "$candidate" ] && return 0
  marker_state=$(awk '
    $0 == "# >>> humansh >>>" {
      starts++
      if (inside || ended) invalid=1
      inside=1
    }
    $0 == "# <<< humansh <<<" {
      ends++
      if (!inside) invalid=1
      inside=0
      ended=1
    }
    END {
      if (inside) invalid=1
      print starts + 0, ends + 0, invalid + 0
    }
  ' "$candidate")
  if [ "$marker_state" != "0 0 0" ] && [ "$marker_state" != "1 1 0" ]; then
    echo "humansh uninstall: managed startup-file markers are corrupted; no startup-file changes were made." >&2
    echo "Fix the markers or run 'humansh doctor --fix', then retry." >&2
    return 1
  fi
}

remove_startup_block() {
  candidate=$1
  candidate_write=$2
  [ ! -f "$candidate" ] && return 0
  startup_mode=$(file_mode "$candidate") || { echo "humansh uninstall: could not determine permissions for $candidate; no startup-file changes were made." >&2; return 1; }
  startup_dir=$(dirname "$candidate_write")
  temp_file=$(mktemp "$startup_dir/.humansh-startup.XXXXXX") || { echo "humansh uninstall: cannot create a temporary file beside $candidate_write; no files were changed." >&2; return 1; }
  trap 'rm -f "$temp_file"' EXIT HUP INT TERM
  awk '
    $0 == "# >>> humansh >>>" { managed=1; next }
    $0 == "# <<< humansh <<<" { managed=0; next }
    !managed { print }
  ' "$candidate" > "$temp_file"
  chmod "$startup_mode" "$temp_file"
  mv "$temp_file" "$candidate_write"
  trap - EXIT HUP INT TERM
}

if [ "$use_zsh" -eq 1 ]; then
  zsh_startup_write=$(resolve_startup "$zsh_startup") || exit 1
  validate_startup "$zsh_startup" || exit 1
fi
if [ "$use_bash" -eq 1 ]; then
  bash_startup_write=$(resolve_startup "$bash_startup") || exit 1
  validate_startup "$bash_startup" || exit 1
fi
if [ "$use_zsh" -eq 1 ]; then remove_startup_block "$zsh_startup" "$zsh_startup_write"; fi
if [ "$use_bash" -eq 1 ]; then remove_startup_block "$bash_startup" "$bash_startup_write"; fi

rm -f "$binary"
[ "$use_zsh" -eq 0 ] || rm -f "$zsh_asset"
[ "$use_bash" -eq 0 ] || rm -f "$bash_asset"
rm -f "$state_file"
rmdir "$(dirname "$zsh_asset")" "$(dirname "$bash_asset")" "$data_dir/shell" "$data_dir" 2>/dev/null || true

if [ "$purge" -eq 1 ]; then
  config_dir=${XDG_CONFIG_HOME:-"$HOME/.config"}/humansh
  case $config_dir in /humansh|humansh) echo "humansh uninstall: refusing unsafe purge path $config_dir." >&2; exit 1 ;; esac
  case $data_dir in /humansh|humansh) echo "humansh uninstall: refusing unsafe purge path $data_dir." >&2; exit 1 ;; esac
  rm -rf "$config_dir"
  rm -rf "$data_dir"
  if [ "$(uname -s)" = Darwin ] && command -v security >/dev/null 2>&1; then
    security delete-generic-password -s humansh.openrouter 2>/dev/null || true
  fi
  echo "humansh uninstalled; configuration and credentials were purged."
else
  echo "humansh uninstalled; configuration and credentials were preserved."
fi

echo "This command cannot alter the parent shell process. If humansh is already loaded there, restart that shell or open a new terminal to unload its in-memory bindings."
