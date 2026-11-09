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

Let's test a real login flow against [DummyJSON](https://dummyjson.com), a free public API made for practice. It needs no sign-up and comes with a demo user. The flow has three stages:

1. log in and receive a token,
2. use the token to read your own profile,
3. create a product.

In `sinq`, a flow like this is a folder, and each stage is one file in it. Files run in name order, which is why they start with numbers.

1. Make a folder for the flow:
   ```bash
   mkdir -p my-tests/login-flow
   cd my-tests/login-flow
   ```

2. Create `01_login.sinq`. The top half is the HTTP request exactly as it goes over the wire. The `$ASSERT` block checks the response, and the `$POST` block saves what later stages need:
   ```http
   ### Log in
   POST https://dummyjson.com/auth/login
   Content-Type: application/json

   {
       "username": "emilys",
       "password": "emilyspass"
   }

   $ASSERT{
       -- The request itself must succeed...
       sinq.assert.code(200, "Login failed")
       -- ...and the response must contain a token we can use.
       sinq.assert.isTrue(type(res.json().accessToken) == "string", "No token in the response")
   }

   $POST{
       -- Keep the token for the next requests. Without `local`, the
       -- variable lives for the whole scenario.
       TOKEN = res.json().accessToken
   }
   ```
   The blocks are written in [Lua](https://www.lua.org/start.html), a small scripting language. `res.json()` reads the response body as JSON, and lines starting with `--` are comments.

3. Create `02_profile.sinq`. `${ TOKEN }` drops the saved token into the header:
   ```http
   ### Read your own profile
   GET https://dummyjson.com/auth/me
   Authorization: Bearer ${ TOKEN }

   $ASSERT{
       sinq.assert.code(200, "Could not read the profile")
       -- We logged in as emilys, so that's who the API should say we are.
       sinq.assert.equals(res.json().username, "emilys", "Logged in as the wrong user")
   }
   ```

4. Create `03_create_product.sinq`:
   ```http
   ### Create a product
   POST https://dummyjson.com/products/add
   Content-Type: application/json

   {
       "title": "Neutrino detector",
       "price": 42
   }

   $ASSERT{
       sinq.assert.code(201, "Product was not created")
       -- The API answers with the product it created, including a new id.
       sinq.assert.equals(res.json().title, "Neutrino detector")
       sinq.assert.isTrue(type(res.json().id) == "number", "The new product has no id")
   }
   ```
   DummyJSON only pretends to save it, so you can run this as often as you like.

## Running `sinq`

Go back to the folder above `my-tests` and point `sinq` at it:

```bash
cd ../..
sinq ./my-tests
```

`sinq` finds one scenario, `login-flow`, runs its three requests in order and prints:

```text
 ✓ PASSED in 994ms | Scenarios: 1✓ | 3 requests sent
```

The process exits with code `0`, which is what CI looks at.

## Break It on Purpose

Change the password in `01_login.sinq` to `"wrong"` and run `sinq ./my-tests` again:

```text
 ✗ login-flow (1✗ 2- in 463ms)
   ✗ Log in (463ms)
     ✗ Failed assertions: Login failed, No token in the response
   - Read your own profile (0ns)
   - Create a product (0ns)

 ✗ FAILED in 464ms | Scenarios: 1✗ | 1 request sent
```

The login fails with both of your messages, and the two later stages don't run (`-`): by default (`fail_fast`), a scenario stops at its first failed stage. The exit code is now `1`, so a CI pipeline would stop here. Put the password back and everything passes again.

From here, every new folder you add under `my-tests` becomes another scenario, and `sinq` runs them all at the same time.

## Next

- **[CLI Reference](cli.md)**: flags, scenario filters, environment variables.
- **[Scenarios & Config](scenario.md)**: chaining requests, passing values between them, `.scenario` files.
- **[Lua API](lua-api.md)**: assertions, JSON, stage lifecycle control.
