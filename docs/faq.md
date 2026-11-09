# Frequently Asked Questions (FAQ)

## Is `sinq` Open Source? Can I contribute?

`sinq` is free and Open Source software, distributed under the GNU General Public License v3 (GPLv3). 

However, at this time, **the project operates on a closed contribution model for code**. While you are free to fork, read, and use the code according to the GPLv3 license, I am currently not accepting code pull requests or feature contributions from the community. This allows me to keep my sanity, as this is a single man project. Possibly subject to change in the future.

Documentation is the exception: pull requests that improve the docs are welcome, under the same GPLv3 license. See [Community](community.md) for how to report bugs, ask questions and contribute.

## How do I pass variables between stages?

You can extract data from a response in the `$POST` hook and assign it to a global variable (by omitting the `local` keyword in Lua).

```lua
$POST{
    local data = res.json()
    -- This saves the token globally for the rest of the scenario
    AUTH_TOKEN = data.token 
}
```

In subsequent stages, you can inject it using inline scripts:

```http
GET ${env.BASE_URL}/protected-route
Authorization: Bearer ${AUTH_TOKEN}
```

## How does concurrency work in `sinq`?

Every leaf directory (a folder with no subfolders) is one scenario. Scenarios run concurrently, up to `-w/--workers` at a time (default 10), and stages within a scenario run in order. Each scenario run gets a fresh Lua state and cookie jar, so there is no shared state to race on.

## Can I share state between different scenarios?

**No.** Every scenario runs in isolation, which is what makes running them in parallel safe. If you need a shared setup stage (like fetching an authentication token or creating a base database record), place that `.sinq` file in a parent directory. Every child scenario will inherit that file and execute the setup independently before running its own specific tests.

## How do I do Data-Driven / Parameterized Testing?

Use the `env_matrix` key in a `.scenario` file. You define an array of environments and `sinq` runs the scenario once per combination. See the [Environment Matrix documentation](env-matrix.md) for examples.

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

If the whole request body **is** the file, use `req.attach()` in `$PRE`. `sinq` streams the file straight from disk into the body:

```http
POST ${env.BASE_URL}/upload
Content-Type: application/octet-stream

$PRE{ req.attach("path/to/my_file.bin") }
```

For a real HTML-style upload (a file plus other form fields), use `req.multipart()` in `$PRE`. Each argument is one part; `filename` makes a part a file part (`true` reuses the source's basename), and the `Content-Type` is sniffed from the extension when omitted:

```http
POST ${env.BASE_URL}/avatar

$PRE{
    req.multipart(
        { name = "user_id", data = env.USER_ID },
        { name = "avatar", source = "fixtures/face.png", filename = true }
    )
}
```

`sinq` sets `Content-Type: multipart/form-data; boundary=…` for you (unless you set a `Content-Type` yourself). See the [Lua API reference](lua-api.md#multipart-bodies) for every entry key.

## How do I send a form (`application/x-www-form-urlencoded`) body?

There is no dedicated helper. Build the body with `sinq.encoding.urlEncode` and set the header explicitly:

```http
POST ${env.BASE_URL}/login
Content-Type: application/x-www-form-urlencoded

${ "username=" .. sinq.encoding.urlEncode(env.USER) .. "&password=" .. sinq.encoding.urlEncode(secrets.PASS) }
```

## How do I enable request caching?

By default, `sinq` fires every request fresh. However, if you have an expensive endpoint that returns the same data and you want to speed up local testing, you can opt-in to caching using the `$PRE` hook:

```http
GET ${env.BASE_URL}/expensive-lookup

$PRE{ req.cache(true) }
```
*Note: Caching is based on the request method, URL, headers and body, plus the cookies sent with it, attached files and the scenario directory. Each retry attempt is cached separately. Responses served from the cache still count toward "requests sent" in the summary.*

To share one response between requests that differ in ways that don't matter, such as an `Accept-Language` from an environment matrix or a trace header, pass a key instead:

```http
POST ${env.BASE_URL}/auth

$PRE{ req.cache("auth:" .. env.phoneNumber) }
```

Every request that uses the same key gets the same cached response, even with a different method, URL or body, so include whatever must keep requests apart (here the phone number) in the key. The first request to fill a key decides what is stored. If it streamed its body to disk with `req.saveResponseTo`, later requests with the same key get an empty body.

## Can I skip a stage conditionally?

Yes. If you determine during the `$PRE` hook that a stage is not needed (e.g., you already have the data or an environment variable disables it), you can mark it to be skipped.

```http
POST ${env.BASE_URL}/optional-step

$PRE{ 
    if env.SKIP_OPTIONAL == "true" then
        req.skip(true)
    end
}
```
Skipped stages bypass the actual network call and all subsequent hooks (`$RETRY`, `$ASSERT`, `$POST`), but are not marked as failures.

## What happens if an API returns a massive payload?

Responses are buffered in memory so Lua can read them, up to `max_body` (a `.scenario` setting, default 1MiB). A larger response is truncated and flagged with `res.oversized = true`.

## How do I download/save a response directly to disk?

If you *expect* a massive payload and want to save it without buffering it in memory (or hitting the `max_body` limit), you can stream it straight to disk using the `$PRE` hook:

```http
GET ${env.BASE_URL}/download-report

$PRE{ req.saveResponseTo("path/to/report.pdf") }
```
With `saveResponseTo`, `res.bodyRaw`, `res.json` and `res.extractBodyJson` are `nil`. `res.size` holds the bytes written, and `sinq.assert.fileMatches` compares the saved file against an expected one.

## How do I handle polling or wait for a job to complete?

You can write retry policies inside the `$RETRY` hook. It executes immediately after every attempt. It must return the number of milliseconds to wait before retrying, or a negative number to stop retrying.

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
The `sinq.retry.*` helpers wrap this pattern, e.g. `sinq.retry.when(condition, delay)`. If an attempt can get no response at all, check `res.error` before calling `res.json()` (see the next question).

## How do I retry when the server is down or slow to start?

`$RETRY` also runs when an attempt gets no response at all. Then `res.error` is set, with a `kind` such as `refused`, `timeout` or `dns`, and the response fields are `nil`:

```http
GET ${env.BASE_URL}/health

$RETRY{
    -- Wait for the service to come up, for at most max_retries attempts
    return sinq.retry.when(res.error ~= nil and res.error.kind == "refused", 1000)
}
```

If the last attempt still got no response, the stage is reported as an error. See [Send Failures](lua-api.md#send-failures-reserror) for the full list of kinds.

## How do I override a deeply nested env/secret key or value?

Use dot-notation; the dots expand into nested objects.

```bash
sinq -e "API.TIMEOUT=500" -s "DB.CREDENTIALS.PASSWORD=secret123"
```

Values are parsed as JSON, so objects and arrays work too:

```bash
sinq -e 'DB.HOSTS=["10.0.0.1", "10.0.0.2"]'
```
