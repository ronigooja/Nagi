package command

import (
	"fmt"
	"strings"
)

func completion(shell string) (string, bool) {
	roots, children := completionCommands()
	switch shell {
	case "bash":
		return rewriteCompletion(shell, bashCompletion(roots, children)), true
	case "zsh":
		return rewriteCompletion(shell, zshCompletion(roots, children)), true
	case "fish":
		return rewriteCompletion(shell, fishCompletion(roots, children)), true
	default:
		return "", false
	}
}

func completionCommands() ([]string, map[string][]string) {
	roots, children := []string{}, map[string][]string{}
	seenRoot, seenChild := map[string]bool{}, map[string]map[string]bool{}
	for _, spec := range commandSpecs {
		parts := strings.Fields(spec.path)
		if len(parts) == 0 {
			continue
		}
		root := parts[0]
		if !seenRoot[root] {
			seenRoot[root] = true
			roots = append(roots, root)
		}
		if len(parts) > 1 {
			if seenChild[root] == nil {
				seenChild[root] = map[string]bool{}
			}
			if !seenChild[root][parts[1]] {
				seenChild[root][parts[1]] = true
				children[root] = append(children[root], parts[1])
			}
		}
	}
	return roots, children
}

func shellWords(v []string) string { return strings.Join(v, " ") }

func bashCompletion(roots []string, children map[string][]string) string {
	var b strings.Builder
	b.WriteString(`_nagi_complete() {
  local cur w root i line skip_lang=0
  local -a semantic=()
  COMPREPLY=()
  cur="${COMP_WORDS[COMP_CWORD]}"
  for ((i=1; i<COMP_CWORD; i++)); do
    w="${COMP_WORDS[i]}"
    if (( skip_lang )); then skip_lang=0; continue; fi
    [[ "$w" == "--json" || "$w" == "--help" || "$w" == "-h" ]] && continue
    if [[ ${#semantic[@]} -eq 0 ]]; then
      if [[ "$w" == --lang ]]; then skip_lang=1; continue; fi
      [[ "$w" == --lang=* ]] && continue
    fi
    semantic+=("$w")
  done
  if (( skip_lang )); then COMPREPLY=( $(compgen -W 'en zh-Hans' -- "$cur") ); return 0; fi
  if [[ ${#semantic[@]} -eq 0 && "$cur" == --lang=* ]]; then
    local language_prefix="${cur#--lang=}" option
    for option in en zh-Hans; do [[ "$option" == "$language_prefix"* ]] && COMPREPLY+=("--lang=$option"); done
    return 0
  fi
  if [[ ${#semantic[@]} -eq 2 && ${semantic[0]} == connections && ${semantic[1]} == watch ]]; then
    COMPREPLY=( $(compgen -W '--interval' -- "$cur") ); return 0
  fi
  if [[ ${semantic[0]} == profile && ${semantic[1]} == override && ${semantic[2]} == apply-default ]]; then
    if [[ ${#semantic[@]} -eq 3 ]]; then
      while IFS= read -r line; do [[ "$line" == "$cur"* ]] && COMPREPLY+=("$line"); done < <(nagi completion candidates profile 2>/dev/null); return 0
    fi
    if [[ ${#semantic[@]} -eq 4 ]]; then COMPREPLY=( $(compgen -W '--dns-proxy' -- "$cur") ); return 0; fi
    if [[ ${#semantic[@]} -eq 5 && ${semantic[4]} == --dns-proxy ]]; then
      while IFS= read -r line; do [[ "$line" == "$cur"* ]] && COMPREPLY+=("$line"); done < <(nagi completion candidates profile-groups "${semantic[3]}" 2>/dev/null); return 0
    fi
  fi
  if [[ ${#semantic[@]} -eq 2 && ${semantic[0]} == rules && ( ${semantic[1]} == remove || ${semantic[1]} == enable || ${semantic[1]} == disable ) ]]; then
    while IFS= read -r line; do [[ "$line" == "$cur"* ]] && COMPREPLY+=("$line"); done < <(nagi completion candidates rules 2>/dev/null); return 0
  fi
  if [[ ${semantic[0]} == rules && ${semantic[1]} == import ]]; then
    if [[ ${#semantic[@]} -eq 3 ]]; then while IFS= read -r line; do COMPREPLY+=("$line"); done < <(compgen -f -- "$cur"); return 0; fi
  fi
  if [[ ${semantic[0]} == proxy && ${semantic[1]} == test ]]; then
    if [[ ${#semantic[@]} -eq 2 ]]; then COMPREPLY=( $(compgen -W 'group provider' -- "$cur") ); return 0; fi
    if [[ ${#semantic[@]} -eq 3 ]]; then
      local source=groups; [[ ${semantic[2]} == provider ]] && source=providers
      while IFS= read -r line; do [[ "$line" == "$cur"* ]] && COMPREPLY+=("$line"); done < <(nagi completion candidates "$source" 2>/dev/null); return 0
    fi
    if [[ ${#semantic[@]} -eq 4 ]]; then
      local source=nodes; [[ ${semantic[2]} == provider ]] && source=provider-nodes
      while IFS= read -r line; do [[ "$line" == "$cur"* ]] && COMPREPLY+=("$line"); done < <(nagi completion candidates "$source" "${semantic[3]}" 2>/dev/null); return 0
    fi
  fi
  if [[ ${semantic[0]} == proxy && ${semantic[1]} == provider && ${semantic[2]} == interval ]]; then
    if [[ ${#semantic[@]} -eq 3 ]]; then COMPREPLY=( $(compgen -W 'show set reset' -- "$cur") ); return 0; fi
    if [[ ${#semantic[@]} -eq 4 ]]; then
      while IFS= read -r line; do [[ "$line" == "$cur"* ]] && COMPREPLY+=("$line"); done < <(nagi completion candidates healthcheck-targets provider 2>/dev/null); return 0
    fi
    if [[ ${#semantic[@]} -eq 5 && ${semantic[3]} == set ]]; then COMPREPLY=( $(compgen -W 'off' -- "$cur") ); return 0; fi
  fi
  if [[ ${semantic[0]} == proxy && ${semantic[1]} == healthcheck ]]; then
    if [[ ${semantic[2]} == url ]]; then
      if [[ ${#semantic[@]} -eq 3 ]]; then COMPREPLY=( $(compgen -W 'set reset' -- "$cur") ); return 0; fi
      if [[ ${#semantic[@]} -eq 4 ]]; then COMPREPLY=( $(compgen -W 'group provider subscription' -- "$cur") ); return 0; fi
      if [[ ${#semantic[@]} -eq 5 ]]; then
        while IFS= read -r line; do [[ "$line" == "$cur"* ]] && COMPREPLY+=("$line"); done < <(nagi completion candidates healthcheck-targets "${semantic[4]}" 2>/dev/null); return 0
      fi
      return 0
    fi
    if [[ ${#semantic[@]} -eq 5 && ${semantic[2]} == show ]]; then COMPREPLY=( $(compgen -W '--full-url' -- "$cur") ); return 0; fi
    if [[ ${#semantic[@]} -eq 2 ]]; then COMPREPLY=( $(compgen -W 'show set reset url' -- "$cur") ); return 0; fi
    if [[ ${#semantic[@]} -eq 3 ]]; then COMPREPLY=( $(compgen -W 'group provider subscription' -- "$cur") ); return 0; fi
    if [[ ${#semantic[@]} -eq 4 ]]; then
      while IFS= read -r line; do [[ "$line" == "$cur"* ]] && COMPREPLY+=("$line"); done < <(nagi completion candidates healthcheck-targets "${semantic[3]}" 2>/dev/null); return 0
    fi
  fi
  if [[ ${semantic[0]} == proxy && ${semantic[1]} == node ]]; then
    if [[ ${semantic[${#semantic[@]}-1]} == --auth-key-file ]]; then while IFS= read -r line; do COMPREPLY+=("$line"); done < <(compgen -f -- "$cur"); return 0; fi
    if [[ ${semantic[${#semantic[@]}-1]} == --group ]]; then while IFS= read -r line; do [[ "$line" == "$cur"* ]] && COMPREPLY+=("$line"); done < <(nagi completion candidates node-groups 2>/dev/null); return 0; fi
    if [[ ${#semantic[@]} -eq 3 && ${semantic[2]} == add ]]; then COMPREPLY=( $(compgen -W 'tailscale' -- "$cur") ); return 0; fi
    if [[ ${#semantic[@]} -eq 3 && ( ${semantic[2]} == show || ${semantic[2]} == edit || ${semantic[2]} == remove ) || ${#semantic[@]} -eq 4 && ${semantic[2]} == groups ]]; then
      while IFS= read -r line; do [[ "$line" == "$cur"* ]] && COMPREPLY+=("$line"); done < <(nagi completion candidates proxy-nodes 2>/dev/null); return 0
    fi
    if [[ ${#semantic[@]} -ge 5 && ${semantic[2]} == groups && ${semantic[3]} == set ]]; then while IFS= read -r line; do [[ "$line" == "$cur"* ]] && COMPREPLY+=("$line"); done < <(nagi completion candidates node-groups 2>/dev/null); return 0; fi
    if [[ "$cur" == --* && ( ${semantic[2]} == add || ${semantic[2]} == edit ) ]]; then local flags='--hostname --control-url --auth-key --auth-key-stdin --auth-key-file'; [[ ${semantic[2]} == edit ]] && flags+=' --exit-node' || flags+=' --group'; COMPREPLY=( $(compgen -W "$flags" -- "$cur") ); return 0; fi
  fi
  if [[ ${semantic[0]} == proxy && ${semantic[1]} == chain ]]; then
    if [[ ${#semantic[@]} -eq 3 ]]; then COMPREPLY=( $(compgen -W 'node provider-node provider subscription' -- "$cur") ); return 0; fi
    if [[ ${semantic[${#semantic[@]}-1]} == --via ]]; then while IFS= read -r line; do [[ "$line" == "$cur"* ]] && COMPREPLY+=("$line"); done < <(nagi completion candidates proxy-nodes 2>/dev/null); return 0; fi
    if [[ "$cur" == --* ]]; then COMPREPLY=( $(compgen -W '--via --via-provider --dry-run' -- "$cur") ); return 0; fi
    if [[ ${#semantic[@]} -ge 4 ]]; then while IFS= read -r line; do [[ "$line" == "$cur"* ]] && COMPREPLY+=("$line"); done < <(nagi completion candidates chain-targets "${semantic[3]}" 2>/dev/null); return 0; fi
  fi
  if [[ "$cur" == --* || "$cur" == -* ]]; then
    COMPREPLY=( $(compgen -W '--json --help -h --lang' -- "$cur") ); return 0
  fi
  if [[ ${#semantic[@]} -eq 0 ]]; then
    COMPREPLY=( $(compgen -W '`)
	b.WriteString(shellWords(roots))
	b.WriteString(`' -- "$cur") ); return 0
  fi
  root="${semantic[0]}"
  if [[ ${#semantic[@]} -eq 2 && "$root" == completion && "${semantic[1]}" == candidates ]]; then
    COMPREPLY=( $(compgen -W 'profile subscription profile-groups groups nodes providers proxy-nodes node-groups chain-targets healthcheck-targets' -- "$cur") ); return 0
  fi
  if [[ ${#semantic[@]} -eq 2 ]]; then
    if [[ "$root" == profile && ( "${semantic[1]}" == use || "${semantic[1]}" == remove ) ]]; then
      while IFS= read -r line; do [[ "$line" == "$cur"* ]] && COMPREPLY+=("$line"); done < <(nagi completion candidates profile 2>/dev/null); return 0
    fi
    if [[ "$root" == subscription && ( "${semantic[1]}" == update || "${semantic[1]}" == apply || "${semantic[1]}" == remove || "${semantic[1]}" == key || "${semantic[1]}" == schedule ) ]]; then
      while IFS= read -r line; do [[ "$line" == "$cur"* ]] && COMPREPLY+=("$line"); done < <(nagi completion candidates subscription 2>/dev/null); return 0
    fi
  fi
  if [[ "$root" == profile && ( "${semantic[1]}" == import || "${semantic[1]}" == export ) && ${#semantic[@]} -eq 3 || "$root" == profile && "${semantic[1]}" == override && "${semantic[2]}" == set && ${#semantic[@]} -eq 4 ]]; then
    while IFS= read -r line; do COMPREPLY+=("$line"); done < <(compgen -f -- "$cur"); return 0
  fi
  if [[ "$root" == subscription && "${semantic[1]}" == key && ${#semantic[@]} -eq 3 ]]; then
    COMPREPLY=( $(compgen -W 'clear' -- "$cur") ); while IFS= read -r line; do COMPREPLY+=("$line"); done < <(compgen -f -- "$cur"); return 0
  fi
  if [[ "$root" == profile && "${semantic[1]}" == override && ${#semantic[@]} -eq 2 ]]; then
    COMPREPLY=( $(compgen -W 'set show clear apply-default default-status restore-default' -- "$cur") ); return 0
  fi
  if [[ "$root" == dns && "${semantic[1]}" == exception && ${#semantic[@]} -eq 2 ]]; then
    COMPREPLY=( $(compgen -W 'add remove' -- "$cur") ); return 0
  fi
  if [[ "$root" == dns && "${semantic[1]}" == tun && ${#semantic[@]} -eq 2 ]]; then
    COMPREPLY=( $(compgen -W 'on off' -- "$cur") ); return 0
  fi
  if [[ "$root" == dns && "${semantic[1]}" == set && ${#semantic[@]} -eq 2 ]]; then
    COMPREPLY=( $(compgen -W 'direct proxy' -- "$cur") ); return 0
  fi
  if [[ "$root" == dns && "${semantic[1]}" == query && ${#semantic[@]} -eq 3 ]]; then
    COMPREPLY=( $(compgen -W 'A AAAA' -- "$cur") ); return 0
  fi
  if [[ "$root" == proxy ]]; then
    if [[ ${#semantic[@]} -eq 2 && "${semantic[1]}" == provider ]]; then
      COMPREPLY=( $(compgen -W 'add show update healthcheck remove interval' -- "$cur") ); return 0
    fi
    if [[ ${#semantic[@]} -eq 3 && "${semantic[1]}" == provider && "${semantic[2]}" != add ]]; then
      while IFS= read -r line; do [[ "$line" == "$cur"* ]] && COMPREPLY+=("$line"); done < <(nagi completion candidates providers 2>/dev/null); return 0
    fi
    if [[ ${#semantic[@]} -eq 5 && "${semantic[1]}" == provider && "${semantic[2]}" == add ]]; then
      while IFS= read -r line; do [[ "$line" == "$cur"* ]] && COMPREPLY+=("$line"); done < <(nagi completion candidates groups 2>/dev/null); return 0
    fi
    local kind="" group=""
    if [[ ${#semantic[@]} -eq 2 ]]; then
      case "${semantic[1]}" in show|select|auto|delays) kind=groups ;; delay) kind=nodes ;; esac
    elif [[ ${#semantic[@]} -eq 3 && "${semantic[1]}" == select ]]; then
      kind=nodes; group="${semantic[2]}"
    fi
    if [[ -n "$kind" ]]; then
      while IFS= read -r line; do [[ "$line" == "$cur"* ]] && COMPREPLY+=("$line"); done < <(nagi completion candidates "$kind" ${group:+"$group"} 2>/dev/null)
      return 0
    fi
  fi
  if [[ ${#semantic[@]} -eq 1 ]]; then
    case "$root" in
`)
	for _, root := range roots {
		if len(children[root]) > 0 {
			fmt.Fprintf(&b, "      %s) COMPREPLY=( $(compgen -W '%s' -- \"$cur\") ) ;;\n", root, shellWords(children[root]))
		}
	}
	b.WriteString("      mode) COMPREPLY=( $(compgen -W 'rule global direct' -- \"$cur\") ) ;;\n    esac\n  fi\n}\ncomplete -F _nagi_complete nagi\n")
	return b.String()
}

func zshCompletion(roots []string, children map[string][]string) string {
	var b strings.Builder
	fmt.Fprintf(&b, `#compdef nagi
_nagi() {
  local -a semantic
  local word i skip_lang=0
  semantic=()
  for ((i=2; i<CURRENT; i++)); do
    word=${words[i]}
    if (( skip_lang )); then skip_lang=0; continue; fi
    [[ $word == --json || $word == --help || $word == -h ]] && continue
    if (( ${#semantic} == 0 )); then
      if [[ $word == --lang ]]; then skip_lang=1; continue; fi
      [[ $word == --lang=* ]] && continue
    fi
    semantic+=("$word")
  done
  if (( skip_lang )); then compadd -- en zh-Hans; return; fi
  if (( ${#semantic} == 0 )) && [[ ${words[CURRENT]} == --lang=* ]]; then
    compset -P '--lang='; compadd -- en zh-Hans; return
  fi
  if (( ${#semantic} == 2 )) && [[ ${semantic[1]} == connections && ${semantic[2]} == watch ]]; then
    compadd -- --interval; return
  fi
  if [[ ${semantic[1]} == profile && ${semantic[2]} == override && ${semantic[3]} == apply-default ]]; then
    if (( ${#semantic} == 3 )); then compadd -- ${(f)"$(nagi completion candidates profile 2>/dev/null)"}; return; fi
    if (( ${#semantic} == 4 )); then compadd -- --dns-proxy; return; fi
    if (( ${#semantic} == 5 )) && [[ ${semantic[5]} == --dns-proxy ]]; then compadd -- ${(f)"$(nagi completion candidates profile-groups "${semantic[4]}" 2>/dev/null)"}; return; fi
  fi
  if (( ${#semantic} == 2 )) && [[ ${semantic[1]} == rules && ( ${semantic[2]} == remove || ${semantic[2]} == enable || ${semantic[2]} == disable ) ]]; then
    compadd -- ${(f)"$(nagi completion candidates rules 2>/dev/null)"}; return
  fi
  if [[ ${semantic[1]} == rules && ${semantic[2]} == import ]]; then
    if (( ${#semantic} == 3 )); then _files; return; fi
  fi
  if [[ ${semantic[1]} == proxy && ${semantic[2]} == test ]]; then
    if (( ${#semantic} == 2 )); then compadd -- group provider; return; fi
    if (( ${#semantic} == 3 )); then
      local source=groups; [[ ${semantic[3]} == provider ]] && source=providers
      compadd -- ${(f)"$(nagi completion candidates "$source" 2>/dev/null)"}; return
    fi
    if (( ${#semantic} == 4 )); then
      local source=nodes; [[ ${semantic[3]} == provider ]] && source=provider-nodes
      compadd -- ${(f)"$(nagi completion candidates "$source" "${semantic[4]}" 2>/dev/null)"}; return
    fi
  fi
  if [[ ${semantic[1]} == proxy && ${semantic[2]} == provider && ${semantic[3]} == interval ]]; then
    if (( ${#semantic} == 3 )); then compadd -- show set reset; return; fi
    if (( ${#semantic} == 4 )); then compadd -- ${(f)"$(nagi completion candidates healthcheck-targets provider 2>/dev/null)"}; return; fi
    if (( ${#semantic} == 5 )) && [[ ${semantic[4]} == set ]]; then compadd -- off; return; fi
  fi
  if [[ ${semantic[1]} == proxy && ${semantic[2]} == healthcheck ]]; then
    if [[ ${semantic[3]} == url ]]; then
      if (( ${#semantic} == 3 )); then compadd -- set reset; return; fi
      if (( ${#semantic} == 4 )); then compadd -- group provider subscription; return; fi
      if (( ${#semantic} == 5 )); then compadd -- ${(f)"$(nagi completion candidates healthcheck-targets "${semantic[5]}" 2>/dev/null)"}; return; fi
      return
    fi
    if (( ${#semantic} == 5 )) && [[ ${semantic[3]} == show ]]; then compadd -- --full-url; return; fi
    if (( ${#semantic} == 2 )); then compadd -- show set reset url; return; fi
    if (( ${#semantic} == 3 )); then compadd -- group provider subscription; return; fi
    if (( ${#semantic} == 4 )); then compadd -- ${(f)"$(nagi completion candidates healthcheck-targets "${semantic[4]}" 2>/dev/null)"}; return; fi
  fi
  if [[ ${semantic[1]} == proxy && ${semantic[2]} == node ]]; then
    if [[ ${semantic[-1]} == --auth-key-file ]]; then _files; return; fi
    if [[ ${semantic[-1]} == --group ]]; then compadd -- ${(f)"$(nagi completion candidates node-groups 2>/dev/null)"}; return; fi
    if (( ${#semantic} == 3 )) && [[ ${semantic[3]} == add ]]; then compadd -- tailscale; return; fi
    if (( ${#semantic} == 3 )) && [[ ${semantic[3]} == show || ${semantic[3]} == edit || ${semantic[3]} == remove ]] || (( ${#semantic} == 4 )) && [[ ${semantic[3]} == groups ]]; then compadd -- ${(f)"$(nagi completion candidates proxy-nodes 2>/dev/null)"}; return; fi
    if (( ${#semantic} >= 5 )) && [[ ${semantic[3]} == groups && ${semantic[4]} == set ]]; then compadd -- ${(f)"$(nagi completion candidates node-groups 2>/dev/null)"}; return; fi
    if [[ ${words[CURRENT]} == --* && ( ${semantic[3]} == add || ${semantic[3]} == edit ) ]]; then compadd -- --hostname --control-url --auth-key --auth-key-stdin --auth-key-file; [[ ${semantic[3]} == edit ]] && compadd -- --exit-node || compadd -- --group; return; fi
  fi
  if [[ ${semantic[1]} == proxy && ${semantic[2]} == chain ]]; then
    if (( ${#semantic} == 3 )); then compadd -- node provider-node provider subscription; return; fi
    if [[ ${semantic[-1]} == --via ]]; then compadd -- ${(f)"$(nagi completion candidates proxy-nodes 2>/dev/null)"}; return; fi
    if [[ ${words[CURRENT]} == --* ]]; then compadd -- --via --via-provider --dry-run; return; fi
    if (( ${#semantic} >= 4 )); then compadd -- ${(f)"$(nagi completion candidates chain-targets "${semantic[4]}" 2>/dev/null)"}; return; fi
  fi
  if [[ ${words[CURRENT]} == -* ]]; then compadd -- --json --help -h --lang; return; fi
  if (( ${#semantic} == 0 )); then compadd -- %s; return; fi
  if (( ${#semantic} == 2 )) && [[ ${semantic[1]} == completion && ${semantic[2]} == candidates ]]; then compadd -- profile subscription profile-groups groups nodes providers proxy-nodes node-groups chain-targets healthcheck-targets; return; fi
  if (( ${#semantic} == 2 )); then
    if [[ ${semantic[1]} == profile && ( ${semantic[2]} == use || ${semantic[2]} == remove ) ]]; then compadd -- ${(f)"$(nagi completion candidates profile 2>/dev/null)"}; return; fi
    if [[ ${semantic[1]} == subscription && ( ${semantic[2]} == update || ${semantic[2]} == apply || ${semantic[2]} == remove || ${semantic[2]} == key || ${semantic[2]} == schedule ) ]]; then compadd -- ${(f)"$(nagi completion candidates subscription 2>/dev/null)"}; return; fi
  fi
  if [[ ${semantic[1]} == profile && ( ${semantic[2]} == import || ${semantic[2]} == export ) && ${#semantic} == 3 || ${semantic[1]} == profile && ${semantic[2]} == override && ${semantic[3]} == set && ${#semantic} == 4 ]]; then _files; return; fi
  if [[ ${semantic[1]} == subscription && ${semantic[2]} == key && ${#semantic} == 4 ]]; then compadd -- clear; _files; return; fi
  if [[ ${semantic[1]} == profile && ${semantic[2]} == override && ${#semantic} == 2 ]]; then compadd -- set show clear apply-default default-status restore-default; return; fi
  if [[ ${semantic[1]} == dns && ${#semantic} == 2 ]]; then case ${semantic[2]} in exception) compadd -- add remove; return ;; tun) compadd -- on off; return ;; set) compadd -- direct proxy; return ;; esac; fi
  if [[ ${semantic[1]} == dns && ${semantic[2]} == query && ${#semantic} == 3 ]]; then compadd -- A AAAA; return; fi
  if [[ ${semantic[1]} == proxy ]]; then
    if (( ${#semantic} == 2 )) && [[ ${semantic[2]} == provider ]]; then compadd -- add show update healthcheck remove interval; return; fi
    if (( ${#semantic} == 3 )) && [[ ${semantic[2]} == provider && ${semantic[3]} != add ]]; then compadd -- ${(f)"$(nagi completion candidates providers 2>/dev/null)"}; return; fi
    if (( ${#semantic} == 5 )) && [[ ${semantic[2]} == provider && ${semantic[3]} == add ]]; then compadd -- ${(f)"$(nagi completion candidates groups 2>/dev/null)"}; return; fi
    local kind="" group=""
    if (( ${#semantic} == 2 )); then
      case ${semantic[2]} in show|select|auto|delays) kind=groups ;; delay) kind=nodes ;; esac
    elif (( ${#semantic} == 3 )) && [[ ${semantic[2]} == select ]]; then kind=nodes; group=${semantic[3]}; fi
    if [[ -n $kind ]]; then
      local -a candidates
      candidates=("${(@f)$(nagi completion candidates "$kind" ${group:+"$group"} 2>/dev/null)}")
      (( ${#candidates} )) && compadd -- "${candidates[@]}"
      return
    fi
  fi
  if (( ${#semantic} == 1 )); then case ${semantic[1]} in
`, shellWords(roots))
	for _, root := range roots {
		if len(children[root]) > 0 {
			fmt.Fprintf(&b, "    %s) compadd -- %s ;;\n", root, shellWords(children[root]))
		}
	}
	b.WriteString("    mode) compadd -- rule global direct ;;\n  esac; fi\n}\ncompdef _nagi nagi\n")
	return b.String()
}

func fishCompletion(roots []string, children map[string][]string) string {
	var b strings.Builder
	b.WriteString(`complete -c nagi -f
complete -c nagi -f -r -n 'test (count (__nagi_semantic)) -eq 0' -l lang -a 'en zh-Hans'
function __nagi_semantic
  set -l out
  set -l skip_lang 0
  for word in (commandline -opc)[2..-1]
    if test $skip_lang -eq 1
      set skip_lang 0
      continue
    end
    if test (count $out) -eq 0
      if test "$word" = --lang
        set skip_lang 1
        continue
      end
      if string match -q -- '--lang=*' "$word"
        continue
      end
    end
    switch $word
      case --json --help -h
      case '*'
        set -a out $word
    end
  end
  if test (count $out) -gt 0
    printf '%s\n' $out
  end
end
`)
	b.WriteString(`complete -c nagi -f -n 'set -l s (__nagi_semantic); test (count $s) -eq 2; and test "$s[1]" = connections; and test "$s[2]" = watch' -l interval -r -d 'Snapshot interval in milliseconds (250–60000)'
complete -c nagi -f -n 'set -l s (__nagi_semantic); test (count $s) -eq 2; and test "$s[1]" = proxy; and test "$s[2]" = test' -a 'group provider'
complete -c nagi -f -n 'set -l s (__nagi_semantic); test (count $s) -eq 3; and test "$s[1]" = proxy; and test "$s[2]" = test; and test "$s[3]" = group' -a '(nagi completion candidates groups 2>/dev/null)'
complete -c nagi -f -n 'set -l s (__nagi_semantic); test (count $s) -eq 3; and test "$s[1]" = proxy; and test "$s[2]" = test; and test "$s[3]" = provider' -a '(nagi completion candidates providers 2>/dev/null)'
complete -c nagi -f -n 'set -l s (__nagi_semantic); test (count $s) -eq 4; and test "$s[1]" = proxy; and test "$s[2]" = test; and test "$s[3]" = group' -a '(set -l s (__nagi_semantic); nagi completion candidates nodes "$s[4]" 2>/dev/null)'
complete -c nagi -f -n 'set -l s (__nagi_semantic); test (count $s) -eq 4; and test "$s[1]" = proxy; and test "$s[2]" = test; and test "$s[3]"  = provider' -a '(set -l s (__nagi_semantic); nagi completion candidates provider-nodes "$s[4]" 2>/dev/null)'
`)
	b.WriteString(`complete -c nagi -f -n 'set -l s (__nagi_semantic); test (count $s) -eq 2; and test "$s[1]" = proxy; and test "$s[2]" = healthcheck' -a 'show set reset url'
complete -c nagi -f -n 'set -l s (__nagi_semantic); test (count $s) -eq 3; and test "$s[1]" = proxy; and test "$s[2]" = healthcheck; and test "$s[3]" != url' -a 'group provider subscription'
complete -c nagi -f -n 'set -l s (__nagi_semantic); test (count $s) -eq 5; and test "$s[1]" = proxy; and test "$s[2]" = healthcheck; and test "$s[3]" = show' -l full-url -d 'Include complete test URL'
complete -c nagi -f -n 'set -l s (__nagi_semantic); test (count $s) -eq 4; and test "$s[1]" = proxy; and test "$s[2]" = healthcheck; and test "$s[3]" != url' -a '(set -l s (__nagi_semantic); nagi completion candidates healthcheck-targets "$s[4]" 2>/dev/null)'
complete -c nagi -f -n 'set -l s (__nagi_semantic); test (count $s) -eq 3; and test "$s[1]" = proxy; and test "$s[2]" = healthcheck; and test "$s[3]" = url' -a 'set reset'
complete -c nagi -f -n 'set -l s (__nagi_semantic); test (count $s) -eq 4; and test "$s[1]" = proxy; and test "$s[2]" = healthcheck; and test "$s[3]" = url' -a 'group provider subscription'
complete -c nagi -f -n 'set -l s (__nagi_semantic); test (count $s) -eq 5; and test "$s[1]" = proxy; and test "$s[2]" = healthcheck; and test "$s[3]" = url' -a '(set -l s (__nagi_semantic); nagi completion candidates healthcheck-targets "$s[5]" 2>/dev/null)'
complete -c nagi -f -n 'set -l s (__nagi_semantic); test (count $s) -eq 3; and test "$s[1]" = proxy; and test "$s[2]" = provider; and test "$s[3]" = interval' -a 'show set reset'
complete -c nagi -f -n 'set -l s (__nagi_semantic); test (count $s) -eq 4; and test "$s[1]" = proxy; and test "$s[2]" = provider; and test "$s[3]" = interval' -a '(nagi completion candidates healthcheck-targets provider 2>/dev/null)'
complete -c nagi -f -n 'set -l s (__nagi_semantic); test (count $s) -eq 5; and test "$s[1]" = proxy; and test "$s[2]" = provider; and test "$s[3]" = interval; and test "$s[4]" = set' -a 'off'

`)
	b.WriteString(`complete -c nagi -f -n 'set -l s (__nagi_semantic); test (count $s) -eq 2; and test "$s[1]" = rules; and contains -- "$s[2]" remove enable disable' -a '(nagi completion candidates rules 2>/dev/null)'
`)
	b.WriteString(`complete -c nagi -n 'set -l s (__nagi_semantic); test (count $s) -eq 3; and test "$s[1]" = rules; and test "$s[2]" = import' -F
`)
	for _, root := range roots {
		fmt.Fprintf(&b, "complete -c nagi -f -n 'test (count (__nagi_semantic)) -eq 0' -a '%s'\n", root)
	}
	for _, root := range roots {
		values := children[root]
		if len(values) == 0 {
			continue
		}
		fmt.Fprintf(&b, "complete -c nagi -f -n 'set -l s (__nagi_semantic); test (count $s) -eq 1; and test \"$s[1]\" = %s' -a '%s'\n", root, shellWords(values))
	}
	b.WriteString("complete -c nagi -f -n 'set -l s (__nagi_semantic); test (count $s) -eq 1; and test \"$s[1]\" = mode' -a 'rule global direct'\n")
	b.WriteString("complete -c nagi -f -n 'set -l s (__nagi_semantic); test (count $s) -eq 2; and test \"$s[1]\" = completion; and test \"$s[2]\" = candidates' -a 'profile subscription profile-groups groups nodes providers proxy-nodes node-groups chain-targets healthcheck-targets'\n")
	b.WriteString("complete -c nagi -f -n 'set -l s (__nagi_semantic); test (count $s) -eq 2; and test \"$s[1]\" = profile; and contains -- $s[2] use remove' -a '(nagi completion candidates profile 2>/dev/null)'\n")
	b.WriteString("complete -c nagi -f -n 'set -l s (__nagi_semantic); test (count $s) -eq 2; and test \"$s[1]\" = subscription; and contains -- $s[2] update apply remove key schedule' -a '(nagi completion candidates subscription 2>/dev/null)'\n")
	b.WriteString("complete -c nagi -n 'set -l s (__nagi_semantic); test (count $s) -eq 3; and test \"$s[1]\" = subscription; and test \"$s[2]\" = key' -F -a clear\n")
	b.WriteString("complete -c nagi -n 'set -l s (__nagi_semantic); test (count $s) -eq 3; and test \"$s[1]\" = profile; and test \"$s[2]\" = import' -F\n")
	b.WriteString("complete -c nagi -n 'set -l s (__nagi_semantic); test (count $s) -eq 3; and test \"$s[1]\" = profile; and test \"$s[2]\" = export' -F\n")
	b.WriteString("complete -c nagi -n 'set -l s (__nagi_semantic); test (count $s) -eq 4; and test \"$s[1]\" = profile; and test \"$s[2]\" = override; and test \"$s[3]\" = set' -F\n")
	b.WriteString("complete -c nagi -f -n 'set -l s (__nagi_semantic); test (count $s) -eq 2; and test \"$s[1]\" = profile; and test \"$s[2]\" = override' -a 'set show clear apply-default default-status restore-default'\n")
	b.WriteString("complete -c nagi -f -n 'set -l s (__nagi_semantic); test (count $s) -eq 3; and test \"$s[1]\" = profile; and test \"$s[2]\" = override; and test \"$s[3]\" = apply-default' -a '(nagi completion candidates profile 2>/dev/null)'\n")
	b.WriteString("complete -c nagi -f -n 'set -l s (__nagi_semantic); test (count $s) -eq 4; and test \"$s[1]\" = profile; and test \"$s[2]\" = override; and test \"$s[3]\" = apply-default' -a '--dns-proxy'\n")
	b.WriteString("complete -c nagi -f -n 'set -l s (__nagi_semantic); test (count $s) -eq 5; and test \"$s[1]\" = profile; and test \"$s[2]\" = override; and test \"$s[3]\" = apply-default; and test \"$s[5]\" = --dns-proxy' -a '(set -l s (__nagi_semantic); nagi completion candidates profile-groups \"$s[4]\" 2>/dev/null)'\n")
	b.WriteString("complete -c nagi -f -n 'set -l s (__nagi_semantic); test (count $s) -eq 2; and test \"$s[1]\" = dns; and test \"$s[2]\" = exception' -a 'add remove'\n")
	b.WriteString("complete -c nagi -f -n 'set -l s (__nagi_semantic); test (count $s) -eq 2; and test \"$s[1]\" = dns; and test \"$s[2]\" = tun' -a 'on off'\n")
	b.WriteString("complete -c nagi -f -n 'set -l s (__nagi_semantic); test (count $s) -eq 2; and test \"$s[1]\" = dns; and test \"$s[2]\" = set' -a 'direct proxy'\n")
	b.WriteString("complete -c nagi -f -n 'set -l s (__nagi_semantic); test (count $s) -eq 3; and test \"$s[1]\" = dns; and test \"$s[2]\" = query' -a 'A AAAA'\n")
	b.WriteString("complete -c nagi -f -n 'set -l s (__nagi_semantic); test (count $s) -eq 2; and test \"$s[1]\" = proxy; and contains -- \"$s[2]\" show select auto delays' -a '(nagi completion candidates groups 2>/dev/null)'\n")
	b.WriteString("complete -c nagi -f -n 'set -l s (__nagi_semantic); test (count $s) -eq 2; and test \"$s[1]\" = proxy; and test \"$s[2]\" = delay' -a '(nagi completion candidates nodes 2>/dev/null)'\n")
	b.WriteString("complete -c nagi -f -n 'set -l s (__nagi_semantic); test (count $s) -eq 3; and test \"$s[1]\" = proxy; and test \"$s[2]\" = select' -a '(set -l s (__nagi_semantic); nagi completion candidates nodes \"$s[3]\" 2>/dev/null)'\n")
	b.WriteString("complete -c nagi -f -n 'set -l s (__nagi_semantic); test (count $s) -eq 2; and test \"$s[1]\" = proxy; and test \"$s[2]\" = provider' -a 'add show update healthcheck remove interval'\n")
	b.WriteString("complete -c nagi -f -n 'set -l s (__nagi_semantic); test (count $s) -eq 5; and test \"$s[1]\" = proxy; and test \"$s[2]\" = provider; and test \"$s[3]\" = add' -a '(nagi completion candidates groups 2>/dev/null)'\n")
	b.WriteString("complete -c nagi -f -n 'set -l s (__nagi_semantic); test (count $s) -eq 3; and test \"$s[1]\" = proxy; and test \"$s[2]\" = provider; and test \"$s[3]\" != add; and test \"$s[3]\" != interval' -a '(nagi completion candidates providers 2>/dev/null)'\n")
	b.WriteString("complete -c nagi -f -l json -l help -s h\n")
	b.WriteString(`complete -c nagi -f -n 'set -l s (__nagi_semantic); test (count $s) -eq 3; and test "$s[1]" = proxy; and test "$s[2]" = node; and test "$s[3]" = add' -a 'tailscale'
complete -c nagi -f -n 'set -l s (__nagi_semantic); test (count $s) -eq 3; and test "$s[1]" = proxy; and test "$s[2]" = node; and contains -- $s[3] show edit remove' -a '(nagi completion candidates proxy-nodes 2>/dev/null)'
complete -c nagi -f -n 'set -l s (__nagi_semantic); test (count $s) -eq 4; and test "$s[1]" = proxy; and test "$s[2]" = node; and test "$s[3]" = groups; and contains -- $s[4] show set clear' -a '(nagi completion candidates proxy-nodes 2>/dev/null)'
complete -c nagi -f -n 'set -l s (__nagi_semantic); test (count $s) -ge 5; and test "$s[1]" = proxy; and test "$s[2]" = node; and test "$s[3]" = groups; and test "$s[4]" = set' -a '(nagi completion candidates node-groups 2>/dev/null)'
complete -c nagi -f -n 'set -l s (__nagi_semantic); test (count $s) -eq 3; and test "$s[1]" = proxy; and test "$s[2]" = chain' -a 'node provider-node provider subscription'
complete -c nagi -f -n 'set -l s (__nagi_semantic); test (count $s) -ge 4; and test "$s[1]" = proxy; and test "$s[2]" = chain' -a '(set -l s (__nagi_semantic); nagi completion candidates chain-targets "$s[4]" 2>/dev/null)'
complete -c nagi -f -n 'set -l s (__nagi_semantic); test "$s[1]" = proxy; and test "$s[2]" = chain; and test "$s[-1]" = --via' -a '(nagi completion candidates proxy-nodes 2>/dev/null)'
complete -c nagi -f -n 'set -l s (__nagi_semantic); test "$s[1]" = proxy; and test "$s[2]" = chain; and contains -- $s[3] set clear' -l dry-run
complete -c nagi -f -r -n 'set -l s (__nagi_semantic); test "$s[1]" = proxy; and test "$s[2]" = chain; and test "$s[3]" = set' -l via
complete -c nagi -f -n 'set -l s (__nagi_semantic); test "$s[1]" = proxy; and test "$s[2]" = node; and contains -- $s[3] add edit' -l auth-key -l auth-key-stdin
complete -c nagi -r -F -n 'set -l s (__nagi_semantic); test "$s[1]" = proxy; and test "$s[2]" = node; and contains -- $s[3] add edit' -l auth-key-file
complete -c nagi -f -r -n 'set -l s (__nagi_semantic); test "$s[1]" = proxy; and test "$s[2]" = node; and contains -- $s[3] add edit' -l hostname -l control-url
complete -c nagi -f -r -n 'set -l s (__nagi_semantic); test "$s[1]" = proxy; and test "$s[2]" = node; and test "$s[3]" = edit' -l exit-node
complete -c nagi -f -r -n 'set -l s (__nagi_semantic); test "$s[1]" = proxy; and test "$s[2]" = node; and test "$s[3]" = add' -l group -a '(nagi completion candidates node-groups 2>/dev/null)'
`)

	return b.String()
}
