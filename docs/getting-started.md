# Getting Started

Welcome to `sinq`! This guide will walk you through installing the tool, writing your first scenario, and running it.

## Installation

You can download the latest pre-compiled binary for your operating system from the [Releases page](https://github.com/Veitangie/sinq/releases) (Windows `.zip`, Debian `.deb`, Fedora `.rpm`). We also maintain official Docker images and Nix flakes.

For quick command-line installation, choose your preferred package manager:

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
    go install github.com/Veitangie/sinq/cmd/sinq@latest
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

`sinq` uses your filesystem to structure workflows. Let's create a very simple health check scenario.

1. Create a directory for your tests:
   ```bash
   mkdir -p my-tests/health
   cd my-tests/health
   ```

2. Create a file named `01_healthcheck.sinq`:
   ```http
   ### Check API Health
   GET https://httpbin.org/get
   
   $ASSERT{
       sinq.assert.equals(res.status, 200)
   }
   ```

## Running `sinq`

Now that you have your first test, let's run it. Point `sinq` to the root directory of your tests:

```bash
sinq run ./my-tests
```

You should see output indicating that the scenario was discovered and the `01_healthcheck.sinq` request passed successfully. 

## Next Steps

Now that you've written a basic test, here is where you should go next:

- **[Lua API](lua-api.md)**: Learn how to script complex assertions, handle JSON parsing, and manipulate request lifecycles.
- **[Scenarios & Config](scenario.md)**: Discover how to chain multiple requests together, pass variables between them, and use `config.scenario` files.
