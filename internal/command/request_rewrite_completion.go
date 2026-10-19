package command

import (
	"fmt"
	"strings"
)

func rewriteCompletion(shell, script string) string {
	if shell == "fish" {
		return script + rewriteFishCompletion()
	}
	// Both shells already remove global help/JSON options into semantic words.
	needle := "  if [[ ${semantic[0]} == profile"
	hook := rewriteBashCompletion
	if shell == "zsh" {
		needle = "  if [[ ${semantic[1]} == profile"
		hook = rewriteZshCompletion
	}
	return strings.Replace(script, needle, hook+"\n"+needle, 1)
}

const rewriteBashCompletion = `  if [[ ${semantic[0]} == request-rewrite || ${semantic[0]} == rewrite ]]; then
    local -a rw=()
    local rp='' prev='' rwkind='' rwchoices='' rwfile=0
    for ((i=0; i<${#semantic[@]}; i++)); do
      w="${semantic[i]}"
      if [[ $prev == --profile ]]; then rp="$w"; prev=''; continue; fi
      if [[ $w == --profile ]]; then prev="$w"; continue; fi
      rw+=("$w")
    done
    if [[ $prev == --profile ]]; then
      while IFS= read -r line; do [[ "$line" == "$cur"* ]] && COMPREPLY+=("$line"); done < <(nagi completion candidates profile 2>/dev/null); return 0
    fi
    if [[ $cur == --* ]]; then
      rwchoices='--json --help'
      case "${rw[1]}" in domains|rules|import|export|validate|preview) rwchoices+=' --profile';; esac
    elif [[ ${#rw[@]} -eq 1 ]]; then rwchoices='status enable disable domains rules import export validate preview ca'
    elif [[ ${#rw[@]} -eq 2 ]]; then
      case "${rw[1]}" in domains) rwchoices='list add update remove';; rules) rwchoices='list show add update remove enable disable reorder';; ca) rwchoices='status generate export trust';; import|export|validate|preview) rwfile=1;; esac
    elif [[ ${#rw[@]} -eq 3 ]]; then
      case "${rw[1]} ${rw[2]}" in
        'ca trust') rwchoices='install remove';;
        'domains update'|'domains remove') rwkind=rewrite-domains;;
        'rules update'|'rules remove'|'rules show'|'rules enable'|'rules disable') rwkind=rewrite-rules;;
        'domains add'|'rules add'|'rules reorder'|'ca export') rwfile=1;;
      esac
    elif [[ ${#rw[@]} -eq 4 && ${rw[2]} == update && ( ${rw[1]} == rules || ${rw[1]} == domains ) ]]; then rwfile=1
    fi
    if [[ -n $rwkind ]]; then
      if [[ -n $rp ]]; then while IFS= read -r line; do [[ "$line" == "$cur"* ]] && COMPREPLY+=("$line"); done < <(nagi completion candidates "$rwkind" "$rp" 2>/dev/null)
      else while IFS= read -r line; do [[ "$line" == "$cur"* ]] && COMPREPLY+=("$line"); done < <(nagi completion candidates "$rwkind" 2>/dev/null); fi
    elif [[ $rwfile == 1 ]]; then
      while IFS= read -r line; do COMPREPLY+=("$line"); done < <(compgen -f -- "$cur")
      [[ - == "$cur"* ]] && COMPREPLY+=('-')
    else COMPREPLY=( $(compgen -W "$rwchoices" -- "$cur") ); fi
    return 0
  fi`
const rewriteZshCompletion = `  if [[ ${semantic[1]} == request-rewrite || ${semantic[1]} == rewrite ]]; then
    local -a rw
    local rp='' prev='' item rwkind='' rwchoices='' rwfile=0
    for item in "${semantic[@]}"; do
      if [[ $prev == --profile ]]; then rp=$item; prev=''; continue; fi
      if [[ $item == --profile ]]; then prev=$item; continue; fi
      rw+=("$item")
    done
    if [[ $prev == --profile ]]; then compadd -- ${(f)"$(nagi completion candidates profile 2>/dev/null)"}; return; fi
    if [[ $words[CURRENT] == --* ]]; then
      rwchoices='--json --help'
      case ${rw[2]} in domains|rules|import|export|validate|preview) rwchoices+=' --profile';; esac
    elif (( ${#rw} == 1 )); then rwchoices='status enable disable domains rules import export validate preview ca'
    elif (( ${#rw} == 2 )); then
      case ${rw[2]} in domains) rwchoices='list add update remove';; rules) rwchoices='list show add update remove enable disable reorder';; ca) rwchoices='status generate export trust';; import|export|validate|preview) rwfile=1;; esac
    elif (( ${#rw} == 3 )); then
      case "${rw[2]} ${rw[3]}" in
        'ca trust') rwchoices='install remove';;
        'domains update'|'domains remove') rwkind=rewrite-domains;;
        'rules update'|'rules remove'|'rules show'|'rules enable'|'rules disable') rwkind=rewrite-rules;;
        'domains add'|'rules add'|'rules reorder'|'ca export') rwfile=1;;
      esac
    elif (( ${#rw} == 4 )) && [[ ${rw[3]} == update && ( ${rw[2]} == domains || ${rw[2]} == rules ) ]]; then rwfile=1
    fi
    if [[ -n $rwkind ]]; then
      if [[ -n $rp ]]; then compadd -- ${(f)"$(nagi completion candidates "$rwkind" "$rp" 2>/dev/null)"}; else compadd -- ${(f)"$(nagi completion candidates "$rwkind" 2>/dev/null)"}; fi
    elif (( rwfile )); then _files; compadd -- -
    else compadd -- ${=rwchoices}; fi
    return
  fi`

func rewriteFishCompletion() string {
	var b strings.Builder
	b.WriteString(`
function __nagi_rewrite_words
  set -l skip 0
  set -l first 1
  for item in (__nagi_semantic)
    if test $skip -eq 1; set skip 0; continue; end
    if test "$item" = --profile; set skip 1; continue; end
    if test "$item" = rewrite; and test $first -eq 1; set item request-rewrite; end
    set first 0
    printf '%s\n' $item
  end
end
function __nagi_rewrite_ids
  set -l s (__nagi_semantic)
  set -l profile
  for i in (seq (count $s))
    if test "$s[$i]" = --profile; set profile $s[(math $i + 1)]; end
  end
  nagi completion candidates $argv[1] $profile 2>/dev/null
end
`)
	for _, group := range []struct{ path, choices string }{{"request-rewrite domains", "list add update remove"}, {"request-rewrite rules", "list show add update remove enable disable reorder"}, {"request-rewrite ca", "status generate export trust"}, {"request-rewrite ca trust", "install remove"}} {
		parts := strings.Fields(group.path)
		conditions := fmt.Sprintf("set -l s (__nagi_rewrite_words); test (count $s) -eq %d", len(parts))
		for i, p := range parts {
			conditions += fmt.Sprintf("; and test \"$s[%d]\" = %s", i+1, p)
		}
		fmt.Fprintf(&b, "complete -c nagi -f -n '%s' -a '%s'\n", conditions, group.choices)
	}
	b.WriteString(`complete -c nagi -f -r -n 'set -l s (__nagi_rewrite_words); test "$s[1]" = request-rewrite; and contains -- "$s[2]" domains rules import export validate preview' -l profile -a '(nagi completion candidates profile 2>/dev/null)'
complete -c nagi -f -n 'set -l s (__nagi_rewrite_words); test (count $s) -eq 3; and test "$s[1]" = request-rewrite; and test "$s[2]" = domains; and contains -- "$s[3]" update remove' -a '(__nagi_rewrite_ids rewrite-domains)'
complete -c nagi -f -n 'set -l s (__nagi_rewrite_words); test (count $s) -eq 3; and test "$s[1]" = request-rewrite; and test "$s[2]" = rules; and contains -- "$s[3]" show update remove enable disable' -a '(__nagi_rewrite_ids rewrite-rules)'
complete -c nagi -F -n 'set -l s (__nagi_rewrite_words); test (count $s) -eq 2; and test "$s[1]" = request-rewrite; and contains -- "$s[2]" import export validate preview' -a '-'
complete -c nagi -F -n 'set -l s (__nagi_rewrite_words); test (count $s) -eq 3; and test "$s[1]" = request-rewrite; and begin; test "$s[2] $s[3]" = "domains add"; or test "$s[2] $s[3]" = "rules add"; or test "$s[2] $s[3]" = "rules reorder"; or test "$s[2] $s[3]" = "ca export"; end' -a '-'
complete -c nagi -F -n 'set -l s (__nagi_rewrite_words); test (count $s) -eq 4; and test "$s[1]" = request-rewrite; and contains -- "$s[2]" domains rules; and test "$s[3]" = update' -a '-'
`)
	return b.String()
}
