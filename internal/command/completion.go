package command

import (
	"fmt"
	"strings"
)

func completion(shell string) (string, bool) {
	roots, children := completionCommands()
	switch shell {
	case "bash":
		return bashCompletion(roots, children), true
	case "zsh":
		return zshCompletion(roots, children), true
	case "fish":
		return fishCompletion(roots, children), true
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
  local cur w root i line
  local -a semantic=()
  COMPREPLY=()
  cur="${COMP_WORDS[COMP_CWORD]}"
  for ((i=1; i<COMP_CWORD; i++)); do
    w="${COMP_WORDS[i]}"
    [[ "$w" == "--json" || "$w" == "--help" || "$w" == "-h" ]] && continue
    semantic+=("$w")
  done
  if [[ "$cur" == --* || "$cur" == -* ]]; then
    COMPREPLY=( $(compgen -W '--json --help -h' -- "$cur") ); return 0
  fi
  if [[ ${#semantic[@]} -eq 0 ]]; then
    COMPREPLY=( $(compgen -W '`)
	b.WriteString(shellWords(roots))
	b.WriteString(`' -- "$cur") ); return 0
  fi
  root="${semantic[0]}"
  if [[ ${#semantic[@]} -eq 2 && "$root" == completion && "${semantic[1]}" == candidates ]]; then
    COMPREPLY=( $(compgen -W 'profile subscription groups nodes' -- "$cur") ); return 0
  fi
  if [[ ${#semantic[@]} -eq 2 ]]; then
    if [[ "$root" == profile && ( "${semantic[1]}" == use || "${semantic[1]}" == remove ) ]]; then
      while IFS= read -r line; do [[ "$line" == "$cur"* ]] && COMPREPLY+=("$line"); done < <(nagi completion candidates profile 2>/dev/null); return 0
    fi
    if [[ "$root" == subscription && ( "${semantic[1]}" == update || "${semantic[1]}" == apply || "${semantic[1]}" == remove ) ]]; then
      while IFS= read -r line; do [[ "$line" == "$cur"* ]] && COMPREPLY+=("$line"); done < <(nagi completion candidates subscription 2>/dev/null); return 0
    fi
  fi
  if [[ "$root" == profile && ( "${semantic[1]}" == import || "${semantic[1]}" == export ) && ${#semantic[@]} -eq 3 || "$root" == profile && "${semantic[1]}" == override && "${semantic[2]}" == set && ${#semantic[@]} -eq 4 ]]; then
    while IFS= read -r line; do COMPREPLY+=("$line"); done < <(compgen -f -- "$cur"); return 0
  fi
  if [[ "$root" == profile && "${semantic[1]}" == override && ${#semantic[@]} -eq 2 ]]; then
    COMPREPLY=( $(compgen -W 'set show clear' -- "$cur") ); return 0
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
    local kind="" group=""
    if [[ ${#semantic[@]} -eq 2 ]]; then
      case "${semantic[1]}" in show|select|delays) kind=groups ;; delay) kind=nodes ;; esac
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
  local word i
  semantic=()
  for ((i=2; i<CURRENT; i++)); do
    word=${words[i]}
    [[ $word == --json || $word == --help || $word == -h ]] && continue
    semantic+=("$word")
  done
  if [[ ${words[CURRENT]} == -* ]]; then compadd -- --json --help -h; return; fi
  if (( ${#semantic} == 0 )); then compadd -- %s; return; fi
  if (( ${#semantic} == 2 )) && [[ ${semantic[1]} == completion && ${semantic[2]} == candidates ]]; then compadd -- profile subscription groups nodes; return; fi
  if (( ${#semantic} == 2 )); then
    if [[ ${semantic[1]} == profile && ( ${semantic[2]} == use || ${semantic[2]} == remove ) ]]; then compadd -- ${(f)"$(nagi completion candidates profile 2>/dev/null)"}; return; fi
    if [[ ${semantic[1]} == subscription && ( ${semantic[2]} == update || ${semantic[2]} == apply || ${semantic[2]} == remove ) ]]; then compadd -- ${(f)"$(nagi completion candidates subscription 2>/dev/null)"}; return; fi
  fi
  if [[ ${semantic[1]} == profile && ( ${semantic[2]} == import || ${semantic[2]} == export ) && ${#semantic} == 3 || ${semantic[1]} == profile && ${semantic[2]} == override && ${semantic[3]} == set && ${#semantic} == 4 ]]; then _files; return; fi
  if [[ ${semantic[1]} == profile && ${semantic[2]} == override && ${#semantic} == 2 ]]; then compadd -- set show clear; return; fi
  if [[ ${semantic[1]} == dns && ${#semantic} == 2 ]]; then case ${semantic[2]} in exception) compadd -- add remove; return ;; tun) compadd -- on off; return ;; set) compadd -- direct proxy; return ;; esac; fi
  if [[ ${semantic[1]} == dns && ${semantic[2]} == query && ${#semantic} == 3 ]]; then compadd -- A AAAA; return; fi
  if [[ ${semantic[1]} == proxy ]]; then
    local kind="" group=""
    if (( ${#semantic} == 2 )); then
      case ${semantic[2]} in show|select|delays) kind=groups ;; delay) kind=nodes ;; esac
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
function __nagi_semantic
  set -l out
  for word in (commandline -opc)[2..-1]
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
	b.WriteString("complete -c nagi -f -n 'set -l s (__nagi_semantic); test (count $s) -eq 2; and test \"$s[1]\" = completion; and test \"$s[2]\" = candidates' -a 'profile subscription groups nodes'\n")
	b.WriteString("complete -c nagi -f -n 'set -l s (__nagi_semantic); test (count $s) -eq 2; and test \"$s[1]\" = profile; and contains -- $s[2] use remove' -a '(nagi completion candidates profile 2>/dev/null)'\n")
	b.WriteString("complete -c nagi -f -n 'set -l s (__nagi_semantic); test (count $s) -eq 2; and test \"$s[1]\" = subscription; and contains -- $s[2] update apply remove' -a '(nagi completion candidates subscription 2>/dev/null)'\n")
	b.WriteString("complete -c nagi -n 'set -l s (__nagi_semantic); test (count $s) -eq 3; and test \"$s[1]\" = profile; and test \"$s[2]\" = import' -F\n")
	b.WriteString("complete -c nagi -n 'set -l s (__nagi_semantic); test (count $s) -eq 3; and test \"$s[1]\" = profile; and test \"$s[2]\" = export' -F\n")
	b.WriteString("complete -c nagi -n 'set -l s (__nagi_semantic); test (count $s) -eq 4; and test \"$s[1]\" = profile; and test \"$s[2]\" = override; and test \"$s[3]\" = set' -F\n")
	b.WriteString("complete -c nagi -f -n 'set -l s (__nagi_semantic); test (count $s) -eq 2; and test \"$s[1]\" = profile; and test \"$s[2]\" = override' -a 'set show clear'\n")
	b.WriteString("complete -c nagi -f -n 'set -l s (__nagi_semantic); test (count $s) -eq 2; and test \"$s[1]\" = dns; and test \"$s[2]\" = exception' -a 'add remove'\n")
	b.WriteString("complete -c nagi -f -n 'set -l s (__nagi_semantic); test (count $s) -eq 2; and test \"$s[1]\" = dns; and test \"$s[2]\" = tun' -a 'on off'\n")
	b.WriteString("complete -c nagi -f -n 'set -l s (__nagi_semantic); test (count $s) -eq 2; and test \"$s[1]\" = dns; and test \"$s[2]\" = set' -a 'direct proxy'\n")
	b.WriteString("complete -c nagi -f -n 'set -l s (__nagi_semantic); test (count $s) -eq 3; and test \"$s[1]\" = dns; and test \"$s[2]\" = query' -a 'A AAAA'\n")
	b.WriteString("complete -c nagi -f -n 'set -l s (__nagi_semantic); test (count $s) -eq 2; and test \"$s[1]\" = proxy; and contains -- \"$s[2]\" show select delays' -a '(nagi completion candidates groups 2>/dev/null)'\n")
	b.WriteString("complete -c nagi -f -n 'set -l s (__nagi_semantic); test (count $s) -eq 2; and test \"$s[1]\" = proxy; and test \"$s[2]\" = delay' -a '(nagi completion candidates nodes 2>/dev/null)'\n")
	b.WriteString("complete -c nagi -f -n 'set -l s (__nagi_semantic); test (count $s) -eq 3; and test \"$s[1]\" = proxy; and test \"$s[2]\" = select' -a '(set -l s (__nagi_semantic); nagi completion candidates nodes \"$s[3]\" 2>/dev/null)'\n")
	b.WriteString("complete -c nagi -f -l json -l help -s h\n")
	return b.String()
}
