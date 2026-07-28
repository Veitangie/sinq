# Alternatives & Comparisons

When deciding on an API testing tool, it's important to choose the right fit for your workflow. Here is a comparison of `sinq` against other popular tools in the space.

## Sinq vs. Hurl

[Hurl](https://hurl.dev) is an incredible tool and arguably the closest in philosophy to `sinq`. Both tools use plain text files to define HTTP requests and assertions, keeping you close to the raw HTTP protocol. 

However, `sinq` diverges in three major ways:

1.  **Full Scripting Language:** Hurl relies on a built-in set of predicates and filters. When you hit a complex edge case (e.g., parsing a specific JWT claim, generating dynamic payloads, or implementing complex retry logic based on response bodies), you might get stuck. `sinq` embeds a complete Lua VM. If you can code it in Lua, you can do it in `sinq`.
2.  **Concurrency by Default:** `sinq` treats every leaf directory as an isolated scenario and runs all of them concurrently out-of-the-box. This drastically reduces execution time for large test suites without requiring external parallelization tools.
3.  **DRY-ness (Don't Repeat Yourself):** In `sinq`, scenarios inherit requests and configuration from parent directories. If 50 endpoints require the same authentication and setup flow, you define it **once** at the root of the folder. Hurl typically requires you to either repeat the setup in each file or pass state manually.

## Sinq vs. Postman / Insomnia

Postman and Insomnia are GUI-first tools for API exploration.

*   **Workflow:** They require you to manually chain requests together or write complex JavaScript in a built-in editor window to extract variables between requests. `sinq` allows you to define requests, assertions, scripts all in your code editor of choice, chaining sequences of requests by default.
*   **Performance:** `sinq` executes independent scenarios concurrently by default.
*   **Version Control:** `sinq` uses plain-text `.sinq` files that live alongside your code. No need for syncing JSON collections, dealing with git conflicts in JSON blobs, or relying on paid team workspaces to collaborate.

## Sinq vs. Bruno

Bruno is a great modern alternative to Postman that stores collections in plain text, making it much better for git version control.

*   **State Management:** Bruno is still fundamentally built around "Collections of Requests". `sinq` is built around workflows - "Scenarios" and directory trees, making it much easier to write complete end-to-end integration flows without managing a massive global environment state.
*   **Scripting:** Bruno relies on Node.js/JavaScript, requiring a heavier runtime footprint. `sinq` embeds a lightweight Lua VM directly into a single compiled binary.

## Sinq vs. REST Client / httpyac

These are fantastic tools for executing requests directly from your editor (VSCode).

*   **CI/CD Focus:** While you can run them in CI, they are primarily built for interactive editor usage. `sinq` is built from the ground-up to be a headless test runner. It features retry logic, native JUnit XML reporting, and environment matrices for parameterized testing, making it better fit for CI/CD pipelines.

## Summary

Choose `sinq` if you want a lightweight, fast, filesystem-based CLI tool designed specifically for writing **stateful HTTP workflows**, staying DRY, and running them in CI/CD pipelines.
