# Getting Started

Install `sinq`, write one scenario, run it.

## Installation

Pre-compiled binaries for each platform are on the [Releases page](https://github.com/Veitangie/sinq/releases) (Windows `.zip`, Debian `.deb`, Fedora `.rpm`), along with Docker images and Nix flakes.

Or via a package manager:

=== "macOS & Linux (Brew)"
    ```bash
    # Build natively
    brew install Veitangie/tap/sinq
    ```

=== "Windows (Scoop)"
    ```powershell
    scoop bucket add veitangie https://github.com/Veitangie/scoop-bucket
    scoop install sinq
    ```

=== "Nix / NixOS"
    ```bash
    nix run github:Veitangie/sinq-nix
    ```

=== "Arch Linux (AUR)"
    ```bash
    yay -S sinq-bin
    ```

=== "Install Script"
    ```bash
    curl -sL https://raw.githubusercontent.com/Veitangie/sinq/refs/heads/main/install.sh | bash
    ```

=== "Go Install"
    ```bash
    go install veitangie.dev/sinq/cmd/sinq@latest
    ```

??? note "Setting up shell auto-completion"
    *(Note: If you installed `sinq` via native Linux packages (`.deb`, `.rpm`, `.apk`), the AUR, or the Homebrew Formula (`brew install Veitangie/tap/sinq`), completions are installed automatically and you can skip this step. For all other methods like the Homebrew Cask (`sinq-bin`), `scoop`, the install script, or `go install`, you'll need to set them up manually).*

    `sinq` supports auto-completion for Bash, Zsh, Fish, and PowerShell. 

    To load completions dynamically in your current session:

    - **Bash/Zsh:** `source <(sinq --completion)`
    - **Fish:** `sinq --completion | source`
    - **PowerShell:** `sinq --completion | Invoke-Expression`

    To install completions permanently, save the output to your shell's completions directory:

    - **Bash:** `sinq --completion > ~/.local/share/bash-completion/completions/sinq`
    - **Zsh:** `sinq --completion > ~/.zfunc/_sinq` (and add `fpath+=~/.zfunc` before `compinit` in `.zshrc`)
    - **Fish:** `sinq --completion > ~/.config/fish/completions/sinq.fish`
    - **PowerShell:** Add `sinq --completion | Invoke-Expression` to your PowerShell profile.

## IDE Integration

To get syntax highlighting and better editor support, check out the community extensions:

- **VSCode / Code OSS**: Install the [sinq-helper](https://marketplace.visualstudio.com/items?itemName=Veitangie.sinq-helper) extension.
- **Other Editors**: Use the [Tree-sitter Grammar](https://github.com/Veitangie/tree-sitter-sinq) for Neovim or any tree-sitter compatible editor.

## Your First Test

`sinq` builds scenarios from your directory layout. Here is a one-request health check.

1. Make a directory for it:
   ```bash
   mkdir -p my-tests/health
   cd my-tests/health
   ```

2. Create `01_healthcheck.sinq`:
   ```http
   ### Check API Health
   GET https://httpbin.org/get

   $ASSERT{
       sinq.assert.code(200, "Healthcheck failed")
   }
   ```

## Running `sinq`

Point `sinq` at the directory:

```bash
sinq ./my-tests
```

`sinq` finds one scenario (`health`), sends its single request, and prints a pass line for it. The process exits `0`. If the assertion fails, the line is red and the exit code is `1`.

## Next

- **[CLI Reference](cli.md)**: flags, scenario filters, environment variables.
- **[Scenarios & Config](scenario.md)**: chaining requests, passing values between them, `.scenario` files.
- **[Lua API](lua-api.md)**: assertions, JSON, request lifecycle control.
