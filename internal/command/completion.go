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
  if [[ "$root" == profile && ( "${semantic[1]}" == import || "${semantic[1]}" == export ) && ${#semantic[@]} -eq 3 || "$root" == profile && "${semantic[1]}" == override && "${semantic[2]}" == set && ${#semantic[@]} -eq 4 ]]; then
    while IFS= read -r line; do COMPREPLY+=("$line"); done < <(compgen -f -- "$cur"); return 0
  fi
  if [[ "$root" == profile && "${semantic[1]}" == override && ${#semantic[@]} -eq 2 ]]; then
    COMPREPLY=( $(compgen -W 'set show clear' -- "$cur") ); return 0
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
	fmt.Fprintf(&b, "#compdef nagi\n_nagi() {\n  local -a semantic\n  local word i\n  semantic=()\n  for ((i=2; i<CURRENT; i++)); do\n    word=${words[i]}\n    [[ $word == --json || $word == --help || $word == -h ]] && continue\n    semantic+=(\"$word\")\n  done\n  if [[ ${words[CURRENT]} == -* ]]; then compadd -- --json --help -h; return; fi\n  if (( ${#semantic} == 0 )); then compadd -- %s; return; fi\n  if [[ ${semantic[1]} == profile && ( ${semantic[2]} == import || ${semantic[2]} == export ) && ${#semantic} == 3 || ${semantic[1]} == profile && ${semantic[2]} == override && ${semantic[3]} == set && ${#semantic} == 4 ]]; then _files; return; fi\n  if [[ ${semantic[1]} == profile && ${semantic[2]} == override && ${#semantic} == 2 ]]; then compadd -- set show clear; return; fi\n  if (( ${#semantic} == 1 )); then case ${semantic[1]} in\n", shellWords(roots))
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
	b.WriteString("complete -c nagi -n 'set -l s (__nagi_semantic); test (count $s) -eq 3; and test \"$s[1]\" = profile; and test \"$s[2]\" = import' -F\n")
	b.WriteString("complete -c nagi -n 'set -l s (__nagi_semantic); test (count $s) -eq 3; and test \"$s[1]\" = profile; and test \"$s[2]\" = export' -F\n")
	b.WriteString("complete -c nagi -n 'set -l s (__nagi_semantic); test (count $s) -eq 4; and test \"$s[1]\" = profile; and test \"$s[2]\" = override; and test \"$s[3]\" = set' -F\n")
	b.WriteString("complete -c nagi -f -n 'set -l s (__nagi_semantic); test (count $s) -eq 2; and test \"$s[1]\" = profile; and test \"$s[2]\" = override' -a 'set show clear'\n")
	b.WriteString("complete -c nagi -f -l json -l help -s h\n")
	return b.String()
}
