# bash completion for sshush
_sshush() {
    local cur prev
    cur="${COMP_WORDS[COMP_CWORD]}"
    prev="${COMP_WORDS[COMP_CWORD-1]}"

    if [ "$COMP_CWORD" -eq 1 ]; then
        COMPREPLY=( $(compgen -W "load-default shell-init restore update version help completion" -- "$cur") )
        return 0
    fi
    if [ "$prev" = "completion" ]; then
        COMPREPLY=( $(compgen -W "bash zsh fish" -- "$cur") )
        return 0
    fi
}
complete -F _sshush sshush
