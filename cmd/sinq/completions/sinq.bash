# Bash completion script for the sinq CLI tool.
#
# To activate these completions in your current shell session, source this file:
#   source /path/to/sinq.bash
#
# To install system-wide or user-wide:
#   - Copy to /etc/bash_completion.d/sinq (Linux system-wide)
#   - Or copy to ~/.local/share/bash-completion/completions/sinq (User-level)

_sinq_completions() {
    local cur prev opts

    COMPREPLY=()

    cur="${COMP_WORDS[COMP_CWORD]}"

    opts="
        -v --version
        -h --help
        -i --insecure
        -V --verbose
        -l --list
        -u --unrestricted
        -p --print
        -w --workers
        -s --secret
        -e --env
        -o --out
        -L --log-level
        -f --format
        -C --color
        -S --show
        -c --count
        -t --tag
        -n --name
        --no-spinner
        --dump-on-failure
        --secrets-file
        --no-tag
        --no-name
        --plugins
        --max-cache-size
        --cache-timeout
    "

    prev="${COMP_WORDS[COMP_CWORD-1]}"

    case "$prev" in
        -L|--log-level)
            COMPREPLY=( $(compgen -W "debug info warn error" -- "$cur") )
            return 0
            ;;
        -f|--format)
            COMPREPLY=( $(compgen -W "std junit" -- "$cur") )
            return 0
            ;;
        -C|--color)
            COMPREPLY=( $(compgen -W "always never auto" -- "$cur") )
            return 0
            ;;
        -S|--show)
            COMPREPLY=( $(compgen -W "all no-skip failed none" -- "$cur") )
            return 0
            ;;
        -o|--out|--secrets-file)
            COMPREPLY=( $(compgen -f -- "$cur") )
            return 0
            ;;
        -w|--workers|-c|--count|-s|--secret|-e|--env|-t|--tag|-n|--name|--no-tag|--no-name|--plugins|--max-cache-size|--cache-timeout)
            return 0
            ;;
    esac

    if [[ "$cur" == -* ]]; then
        COMPREPLY=( $(compgen -W "$opts" -- "$cur") )
        return 0
    fi

    COMPREPLY=( $(compgen -f -- "$cur") )
}

complete -F _sinq_completions sinq
