# Frequently Asked Questions (FAQ)

Here are some of the most common questions regarding `sinq`.

## Is `sinq` Open Source? Can I contribute?

`sinq` is free and Open Source software, distributed under the GNU General Public License v3 (GPLv3). 

However, at this time, **the project operates on a closed contribution model**. While you are free to fork, read, and use the code according to the GPLv3 license, I am currently not accepting pull requests or feature contributions from the community. This allows me to keep my sanity, as this is a single man project. Possibly subject to change in the future.

## How do I pass variables between requests?

You can extract data from a response in the `$POST` hook and assign it to a global variable (by omitting the `local` keyword in Lua).

```lua
$POST{
    local data = res.json()
    -- This saves the token globally for the rest of the scenario
    AUTH_TOKEN = data.token 
}
```

In subsequent requests, you can inject it using inline scripts:

```http
GET ${env.BASE_URL}/protected-route
Authorization: Bearer ${AUTH_TOKEN}
```

## How does concurrency work in `sinq`?

When you point `sinq` at a root directory, it recursively scans for files. **Every leaf directory (a folder with no subfolders) becomes an isolated "Scenario".** 

`sinq` runs all of these Scenarios concurrently by default, bounded only by the `-w/--workers` flag (which defaults to 10). Because each Scenario gets its own dedicated Lua VM and environment sandbox, they run entirely in parallel without any race conditions.

## Can I share state between different scenarios?

**No.** By design, every scenario runs in complete isolation. This is what guarantees that your tests can be run safely in parallel. If you need a shared setup step (like fetching an authentication token or creating a base database record), place that `.sinq` file in a parent directory. Every child scenario will inherit that file and execute the setup independently before running its own specific tests.

## How do I do Data-Driven / Parameterized Testing?

`sinq` supports this natively through the `env_matrix` configuration in `.scenario` files. You can define an array of environments, and `sinq` will automatically duplicate and execute the scenario once for each environment payload in the matrix. See the [Environment Matrix documentation](env-matrix.md) for detailed examples.

## How do I handle secrets securely?

Never hardcode secrets (like API keys or passwords) in your `.sinq` files. Instead, use the `--secrets-file` argument to point to a JSON file containing your secrets, or pass them inline using `-s KEY=VALUE` (supports dot-notation and JSON values, e.g. `-s API.KEY=123`). 

You can then access them safely in your Lua scripts via the `secrets` table:

```http
POST ${env.BASE_URL}/login

{
  "username": "admin",
  "password": "${secrets.ADMIN_PASS}"
}
```

## How do I upload a file?

To send raw file contents as the request body, use the `$PRE` hook's `req.attach()` method. This instructs `sinq` to stream the file directly from disk into the HTTP request body.

```http
POST ${env.BASE_URL}/upload
Content-Type: application/octet-stream

$PRE{ req.attach("path/to/my_file.bin") }
```

## How do I enable request caching?

By default, `sinq` fires every request fresh. However, if you have an expensive endpoint that returns the same data and you want to speed up local testing, you can opt-in to caching using the `$PRE` hook:

```http
GET ${env.BASE_URL}/expensive-lookup

$PRE{ req.cache(true) }
```
*Note: Caching is based on the request method, URL, headers, and body.*

## Can I skip a request conditionally?

Yes. If you determine during the `$PRE` hook that a request is not needed (e.g., you already have the data or an environment variable disables it), you can mark it to be skipped.

```http
POST ${env.BASE_URL}/optional-step

$PRE{ 
    if env.SKIP_OPTIONAL == "true" then
        req.skip(true)
    end
}
```
Skipped requests bypass the actual network call and all subsequent hooks (`$RETRY`, `$ASSERT`, `$POST`), but are not marked as failures.

## What happens if an API returns a massive payload?

`sinq` buffers responses in memory by default so that Lua scripts can parse them. To prevent out-of-memory crashes on massive payloads (like downloading a 2GB ISO), `sinq` enforces a `max_body` setting (configurable in `.scenario` files, defaults to 1MiB). 

If a response exceeds this limit, `sinq` safely truncates it and flags the response with `oversized = true`.

## How do I download/save a response directly to disk?

If you *expect* a massive payload and want to save it without buffering it in memory (or hitting the `max_body` limit), you can stream it straight to disk using the `$PRE` hook:

```http
GET ${env.BASE_URL}/download-report

$PRE{ req.saveResponseTo("path/to/report.pdf") }
```
When using `saveResponseTo`, the `res.bodyRaw` and `res.bodyJson` fields will remain empty to save memory.

## How do I handle polling or wait for a job to complete?

You can write retry policies inside the `$RETRY` hook. It executes immediately after the response is received. It must return the number of milliseconds to wait before retrying, or a negative number to stop retrying.

```http
GET ${env.BASE_URL}/job-status

$RETRY{ 
    -- If status is pending, retry in 500ms
    if res.json().status == "pending" then
        return 500
    end
    -- Stop retrying otherwise
    return -1
}
```
You can also use the built-in helpers like `sinq.retry.when(condition, delay)` for cleaner code!

## How do I override a deeply nested env/secret key or value?

If you have a complex configuration object and you need to override a specific nested key from the command line, you can use dot-notation. `sinq` will automatically expand the dots into a nested JSON object structure.

```bash
sinq -e "API.TIMEOUT=500" -s "DB.CREDENTIALS.PASSWORD=secret123"
```

Additionally, because the CLI arguments are unmarshaled via JSON under the hood, you can pass entire JSON objects or arrays as values:

```bash
sinq -e 'DB.HOSTS=["10.0.0.1", "10.0.0.2"]'
```
