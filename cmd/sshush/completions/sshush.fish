# fish completion for sshush
complete -c sshush -f

complete -c sshush -n __fish_use_subcommand -a load-default -d 'load default identities into the agent'
complete -c sshush -n __fish_use_subcommand -a shell-init -d 'print a shell snippet to load the default on shell start'
complete -c sshush -n __fish_use_subcommand -a restore -d 'revert the SSH config to the backup from before edits'
complete -c sshush -n __fish_use_subcommand -a update -d 'update sshush to the latest release'
complete -c sshush -n __fish_use_subcommand -a version -d 'print the installed version'
complete -c sshush -n __fish_use_subcommand -a help -d 'show help'
complete -c sshush -n __fish_use_subcommand -a completion -d 'print a shell completion script'

complete -c sshush -n '__fish_seen_subcommand_from completion' -a 'bash zsh fish'
