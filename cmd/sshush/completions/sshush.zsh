#compdef sshush
# zsh completion for sshush

_sshush() {
    local -a commands
    commands=(
        'load-default:load the configured default identities into the agent'
        'shell-init:print a shell snippet to load the default on shell start'
        'restore:revert the SSH config to the backup from before edits'
        'update:update sshush to the latest release'
        'version:print the installed version'
        'help:show help'
        'completion:print a shell completion script (bash|zsh|fish)'
        'install-extras:install man page and completions'
    )

    if (( CURRENT == 2 )); then
        _describe -t commands 'sshush command' commands
        return
    fi
    if (( CURRENT == 3 )) && [[ ${words[2]} == completion ]]; then
        _values 'shell' bash zsh fish
        return
    fi
}

_sshush "$@"
