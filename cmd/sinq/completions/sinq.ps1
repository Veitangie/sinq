# PowerShell completion script for the sinq CLI tool.
#
# To activate, add the following to your PowerShell profile ($PROFILE):
#   . /path/to/sinq.ps1

Register-ArgumentCompleter -CommandName sinq -Native -ScriptBlock {
    param($wordToComplete, $commandAst, $cursorPosition)

    $tokens = $commandAst.ToString() -split '\s+'
    $prev = if ($tokens.Count -ge 2) { $tokens[-2] } else { '' }

    $choiceMap = @{
        '-L'          = @('debug', 'info', 'warn', 'error')
        '--log-level' = @('debug', 'info', 'warn', 'error')
        '-f'          = @('std', 'junit')
        '--format'    = @('std', 'junit')
        '-c'          = @('always', 'never', 'auto')
        '--color'     = @('always', 'never', 'auto')
        '-S'          = @('all', 'no-skip', 'failed')
        '--show'      = @('all', 'no-skip', 'failed')
    }

    if ($choiceMap.ContainsKey($prev)) {
        $choiceMap[$prev] | Where-Object { $_ -like "$wordToComplete*" } | ForEach-Object {
            [System.Management.Automation.CompletionResult]::new($_, $_, 'ParameterValue', $_)
        }
        return
    }

    # Flags with file path values
    $fileFlags = @('-o', '--out', '--secrets-file')
    if ($fileFlags -contains $prev) {
        Get-ChildItem -Path "$wordToComplete*" -ErrorAction SilentlyContinue | ForEach-Object {
            [System.Management.Automation.CompletionResult]::new($_.FullName, $_.Name, 'ProviderItem', $_.FullName)
        }
        return
    }

    $valueFlags = @('-w', '--workers', '-s', '--secret', '-e', '--env', '-t', '--tag',
                    '-n', '--name', '--skip-tag', '--skip-name', '--plugins',
                    '--max-cache-size', '--cache-timeout')
    if ($valueFlags -contains $prev) {
        return
    }

    if ($wordToComplete -like '-*') {
        $flags = @(
            @{ Name = '-v';                Tip = 'Print the current sinq version and exit' },
            @{ Name = '--version';         Tip = 'Print the current sinq version and exit' },
            @{ Name = '-h';                Tip = 'Print this help message and exit' },
            @{ Name = '--help';            Tip = 'Print this help message and exit' },
            @{ Name = '-w';                Tip = 'Number of concurrent workers' },
            @{ Name = '--workers';         Tip = 'Number of concurrent workers' },
            @{ Name = '-i';                Tip = 'Disable SSL/TLS certificate verification' },
            @{ Name = '--insecure';        Tip = 'Disable SSL/TLS certificate verification' },
            @{ Name = '-s';                Tip = 'Key=value overrides for scenario secrets' },
            @{ Name = '--secret';          Tip = 'Key=value overrides for scenario secrets' },
            @{ Name = '-e';                Tip = 'Key=value overrides for all scenario environments' },
            @{ Name = '--env';             Tip = 'Key=value overrides for all scenario environments' },
            @{ Name = '-o';                Tip = 'Path to write the output file' },
            @{ Name = '--out';             Tip = 'Path to write the output file' },
            @{ Name = '-L';                Tip = 'Log level to use: debug, info, warn or error' },
            @{ Name = '--log-level';       Tip = 'Log level to use: debug, info, warn or error' },
            @{ Name = '-f';                Tip = 'Output format: std or junit' },
            @{ Name = '--format';          Tip = 'Output format: std or junit' },
            @{ Name = '-V';                Tip = 'Enable verbose reporting' },
            @{ Name = '--verbose';         Tip = 'Enable verbose reporting' },
            @{ Name = '-c';                Tip = 'Terminal colors: always, never, auto' },
            @{ Name = '--color';           Tip = 'Terminal colors: always, never, auto' },
            @{ Name = '-S';                Tip = 'Which results to show in the output: all, no-skip, failed' },
            @{ Name = '--show';            Tip = 'Which results to show in the output: all, no-skip, failed' },
            @{ Name = '-l';                Tip = 'Parse and list scenarios at specified directories' },
            @{ Name = '--list';            Tip = 'Parse and list scenarios at specified directories' },
            @{ Name = '-t';                Tip = 'Execute only scenarios that have the tag' },
            @{ Name = '--tag';             Tip = 'Execute only scenarios that have the tag' },
            @{ Name = '-n';                Tip = 'Execute only scenarios which names match the regular expression' },
            @{ Name = '--name';            Tip = 'Execute only scenarios which names match the regular expression' },
            @{ Name = '-u';                Tip = 'Load lua "os" and "io" modules for scripts' },
            @{ Name = '--unrestricted';    Tip = 'Load lua "os" and "io" modules for scripts' },
            @{ Name = '--secrets-file';    Tip = 'Path to JSON-formatted secrets file' },
            @{ Name = '--skip-tag';        Tip = 'Do not execute scenarios that have the tag' },
            @{ Name = '--skip-name';       Tip = 'Do not execute scenarios which names match the regular expression' },
            @{ Name = '--plugins';         Tip = 'Paths to lua plugin directory entries' },
            @{ Name = '--max-cache-size';  Tip = 'Global maximum response size for cached requests' },
            @{ Name = '--cache-timeout';   Tip = 'Global timeout for the cached requests' },
            @{ Name = '--dump-on-failure'; Tip = 'Print full request and response data on failed assertion' }
        )

        $flags | Where-Object { $_.Name -like "$wordToComplete*" } | ForEach-Object {
            [System.Management.Automation.CompletionResult]::new($_.Name, $_.Name, 'ParameterName', $_.Tip)
        }
        return
    }

    Get-ChildItem -Directory -Path "$wordToComplete*" -ErrorAction SilentlyContinue | ForEach-Object {
        [System.Management.Automation.CompletionResult]::new($_.FullName, $_.Name, 'ProviderItem', $_.FullName)
    }
}
