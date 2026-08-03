# Fish completion script for the sinq CLI tool.
#
# This file is automatically loaded by Fish when placed in:
#   /usr/share/fish/vendor_completions.d/sinq.fish  (system-wide)
#   ~/.config/fish/completions/sinq.fish             (user-level)

complete -c sinq -f

complete -c sinq -s v -l version    -d 'Print the current sinq version and exit'
complete -c sinq -s h -l help       -d 'Print this help message and exit'
complete -c sinq -s i -l insecure   -d 'Disable SSL/TLS certificate verification'
complete -c sinq -s V -l verbose    -d 'Enable verbose reporting'
complete -c sinq -s l -l list       -d 'Parse and list scenarios at specified directories'
complete -c sinq -s u -l unrestricted -d 'Load lua "os" and "io" modules for scripts'
complete -c sinq      -l dump-on-failure -d 'Print full request and response data on failed assertion'

complete -c sinq -s L -l log-level -x -a 'debug info warn error' -d 'Log level to use: debug, info, warn or error'
complete -c sinq -s f -l format   -x -a 'std junit'              -d 'Output format: std or junit'
complete -c sinq -s c -l color    -x -a 'always never auto'      -d 'Terminal colors: always, never, auto'
complete -c sinq -s S -l show     -x -a 'all no-skip failed'     -d 'Which results to show in the output: all, no-skip, failed'

complete -c sinq -s o -l out          -r -F -d 'Path to write the output file'
complete -c sinq      -l secrets-file -r -F -d 'Path to JSON-formatted secrets file'

complete -c sinq -s w -l workers        -r -d 'Number of concurrent workers'
complete -c sinq -s s -l secret         -r -d 'Key=value overrides for scenario secrets'
complete -c sinq -s e -l env            -r -d 'Key=value overrides for all scenario environments'
complete -c sinq -s t -l tag            -r -d 'Execute only scenarios that have the tag'
complete -c sinq -s n -l name           -r -d 'Execute only scenarios which names match the regex'
complete -c sinq      -l skip-tag       -r -d 'Do not execute scenarios that have the tag'
complete -c sinq      -l skip-name      -r -d 'Do not execute scenarios which names match the regex'
complete -c sinq      -l plugins        -r -d 'Paths to lua plugin directory entries'
complete -c sinq      -l max-cache-size -r -d 'Global maximum response size for cached requests'
complete -c sinq      -l cache-timeout  -r -d 'Global timeout for the cached requests'

complete -c sinq -a '(__fish_complete_directories)'
