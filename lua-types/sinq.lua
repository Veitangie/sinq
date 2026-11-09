---@meta

-- sinq - A concurrent integration testing tool
-- Copyright (C) 2026 Veitangie
-- SPDX-License-Identifier: GPL-3.0-or-later

-- Type definitions for the sinq Lua API, for lua-language-server (LuaCATS annotations).
-- Definitions only: sinq never loads this file, and calling these stubs does nothing.
-- Full behaviour: https://sinq.veitangie.dev/lua-api
--
-- Add this directory to your workspace and match sinq's Lua version in .luarc.json:
--   { "runtime.version": "Lua 5.1", "workspace.library": ["path/to/lua-types"] }

---@alias sinq.HashEncoding "hex"|"base64"|"base64url"|"raw"
---@alias sinq.HmacAlgorithm "sha256"|"sha1"|"sha512"|"md5"
---@alias sinq.ErrorKind "timeout"|"dns"|"refused"|"reset"|"tls"|"redirects"|"other"|string

---@class sinq.SendError
---@field kind sinq.ErrorKind Branch on the named kinds; `other` may be split in a minor release.
---@field message string Human-readable; the wording may change between releases.

---Set by sinq after each attempt. On an attempt that got no response only `attempt` and
---`error` are set; `code`, `headers`, `bodyRaw`, `json` and `extractBodyJson` are nil then.
---They aren't marked optional here to spare every plugin a nil check, so check `error` first
---when a request can fail. `bodyRaw`, `json` and `extractBodyJson` are also nil after
---`req.saveResponseTo`.
---@class sinq.Response
---@field attempt integer The current attempt, counting from 1.
---@field code integer HTTP status code.
---@field headers table<string, string|string[]> A repeated header is an array of its values.
---@field bodyRaw string Raw response body.
---@field package bodyJson? any Internal cache behind `json` and `extractBodyJson`; not part of the API, may be removed in any release.
---@field oversized? boolean True if the body exceeded `max_body` and was truncated.
---@field size? integer Bytes written to disk by `req.saveResponseTo`.
---@field error? sinq.SendError Set when the attempt got no response.
---@field json fun(): any Parses the body as JSON, raising an error if it isn't valid.
---@field extractBodyJson fun(): any?, string? Parses the body as JSON: (result, error).

---One part of a multipart body. `name` is required unless `contentDisposition` is set.
---`data` and `source` are mutually exclusive.
---@class sinq.MultipartEntry
---@field name? string Form field name.
---@field data? string Literal part body.
---@field source? string Path of a file to send, relative to the current `.sinq` file.
---@field filename? string|boolean Advertised filename; `true` uses `basename(source)`.
---@field contentType? string The part's Content-Type.
---@field contentDisposition? string Raw Content-Disposition header; `name` and `filename` are ignored.
---@field headers? table<string, string|number|boolean> Extra headers for this part.

---@class sinq.DecodedJwt
---@field header table<string, any>
---@field claims table<string, any>
---@field signature string
---@field method string

---Environment of the current scenario, including `-e` / `--env` overrides.
---@type table<string, any>
env = {}

---Values from `-s` / `--secret` / `--secrets-file`.
---@type table<string, any>
secrets = {}

---The current response: `sinq.responses[<current stage>]`. Empty until an attempt finished,
---In a stage that only runs scripts, reading or writing it raises an error.
---@type sinq.Response
res = {}

---The current request. Its functions only exist during `$PRE`.
req = {}

---Uses a file's contents as the whole request body.
---@param path string Relative to the current `.sinq` file.
function req.attach(path) end

---Streams the response body to disk instead of buffering it.
---@param path string Relative to the current `.sinq` file.
function req.saveResponseTo(path) end

---Turns response caching on (default) or off, or sets an explicit cache key.
---@param enableOrKey? boolean|string With a string, every request using that key shares one response.
function req.cache(enableOrKey) end

---Marks the stage to be skipped. The only `req` function allowed in a stage without a request;
---the others raise an error there.
---@param enable? boolean Defaults to true.
function req.skip(enable) end

---Adds parts to a `multipart/form-data` body. Repeated calls accumulate.
---@param entry sinq.MultipartEntry|sinq.MultipartEntry[] A part, or an array of parts.
---@param ... sinq.MultipartEntry More parts.
function req.multipart(entry, ...) end

---Replaces the random multipart boundary. Validated when the body is assembled.
---@param boundary string
function req.multipartBoundary(boundary) end

sinq = {}

---Responses of the scenario so far, by stage number (1-indexed). A stage that only runs
---scripts has no entry.
---@type table<integer, sinq.Response>
sinq.responses = {}

---Runs the stage at `index` (1-indexed) next, once the current one finishes.
---@param index integer
function sinq.setNextStage(index) end

---Ends the scenario once the current stage finishes.
function sinq.finishScenario() end

---Only exists during `$RETRY`. A `$RETRY` script must return the milliseconds to wait
---before the next attempt, or a negative number to stop.
sinq.retry = {}

---Return this to stop retrying.
sinq.retry.stop = -1

---@param condition any Retries while truthy.
---@param delay? number Milliseconds, 500 by default.
---@return number delay Milliseconds to wait, or -1 to stop.
function sinq.retry.when(condition, delay) end

---Retries with a delay of `base ^ res.attempt * constant`.
---@param condition any Retries while truthy.
---@param base? number 2 by default. Anything outside (0, 10] falls back to 2.
---@param constant? number Milliseconds, 500 by default.
---@return number delay Milliseconds to wait, or -1 to stop.
function sinq.retry.whenExponential(condition, base, constant) end

---Adds random jitter to the delay `delegate` returns.
---@param condition any Retries while truthy.
---@param range? number Jitter of +/- this many milliseconds, 50 by default.
---@param delegate? fun(condition: any, ...: any): number `sinq.retry.when` by default.
---@param ... any Passed to `delegate` after `condition`.
---@return number delay Milliseconds to wait, or a negative number to stop.
function sinq.retry.withJitter(condition, range, delegate, ...) end

---Only exists during `$ASSERT`.
sinq.assert = {}

---Records a failure. Execution continues, so one block can report several.
---@param reason string
function sinq.assert.fail(reason) end

---@param expected integer
---@param message? string Replaces the default failure text.
function sinq.assert.code(expected, message) end

---Compares values. A table in `expected` is a partial match: extra keys in `actual` are ignored.
---@param actual any
---@param expected any
---@param message? string Replaces the default failure text.
function sinq.assert.equals(actual, expected, message) end

---@param source string
---@param substring string
---@param message? string Replaces the default failure text.
function sinq.assert.contains(source, substring, message) end

---@param condition any Fails on `false` or `nil`.
---@param message? string Replaces the default failure text.
function sinq.assert.isTrue(condition, message) end

---Fails if the file saved by `req.saveResponseTo` differs from `path`.
---@param path string Relative to the current `.sinq` file.
function sinq.assert.fileMatches(path) end

sinq.json = {}

---Represents a JSON `null`, both when serializing and in parsed JSON.
---@type userdata
sinq.json.null = nil

---@param source string
---@return any? result
---@return string? error
function sinq.json.parse(source) end

---@param value any
---@param indent? string Pretty-prints with this indent; compact JSON if omitted.
---@return string? result
---@return string? error
function sinq.json.serialize(value, indent) end

---Marks a table as an array, so it serializes as `[]` when empty.
---@param tbl? table A new empty array if omitted.
---@return table? result
---@return string? error
function sinq.json.array(tbl) end

sinq.crypto = {}

---@param source string
---@param encoding? sinq.HashEncoding "hex" by default.
---@return string? result
---@return string? error
function sinq.crypto.md5(source, encoding) end

---@param source string
---@param encoding? sinq.HashEncoding "hex" by default.
---@return string? result
---@return string? error
function sinq.crypto.sha1(source, encoding) end

---@param source string
---@param encoding? sinq.HashEncoding "hex" by default.
---@return string? result
---@return string? error
function sinq.crypto.sha256(source, encoding) end

---@param source string
---@param encoding? sinq.HashEncoding "hex" by default.
---@return string? result
---@return string? error
function sinq.crypto.sha512(source, encoding) end

---@param source string
---@param algo? sinq.HmacAlgorithm "sha256" by default.
---@param key? string Empty by default.
---@param encoding? sinq.HashEncoding "hex" by default.
---@return string? result
---@return string? error
function sinq.crypto.hmac(source, algo, key, encoding) end

sinq.encoding = {}

---@param source string
---@return string
function sinq.encoding.base64Encode(source) end

---@param source string
---@return string? result
---@return string? error
function sinq.encoding.base64Decode(source) end

---@param source string
---@return string
function sinq.encoding.base64UrlEncode(source) end

---@param source string
---@return string? result
---@return string? error
function sinq.encoding.base64UrlDecode(source) end

---@param source string
---@return string
function sinq.encoding.hexEncode(source) end

---@param source string
---@return string? result
---@return string? error
function sinq.encoding.hexDecode(source) end

---Percent-encodes for a query string or form body. Spaces become `+`.
---@param source string
---@return string
function sinq.encoding.urlEncode(source) end

---@param source string
---@return string? result
---@return string? error
function sinq.encoding.urlDecode(source) end

sinq.time = {}

sinq.time.ms = 1
sinq.time.second = 1000
sinq.time.minute = 60000
sinq.time.hour = 3600000

---@return number now UNIX time in milliseconds.
function sinq.time.now() end

---Parses a time string. One with no time zone is read as UTC.
---@param source string
---@param format? string A Go time layout. ISO8601 by default.
---@return number? result UNIX time in milliseconds.
---@return string? error
function sinq.time.fromString(source, format) end

---Formats a timestamp in UTC.
---@param ms integer UNIX time in milliseconds.
---@param format? string A Go time layout. ISO8601 by default.
---@return string
function sinq.time.toString(ms, format) end

---Blocks the script. Counts against `script_timeout`.
---@param ms integer
function sinq.time.sleep(ms) end

sinq.jwt = {}

---Decodes a token without validating its signature.
---@param token string
---@return sinq.DecodedJwt? result
---@return string? error
function sinq.jwt.decode(token) end

---@param token string
---@param key string A raw key for `HS*`, a PEM public key for `RS*`, `ES*` and `EdDSA`.
---@param algo? string Required algorithm. If omitted, the token's own `alg` header is used.
---@return sinq.DecodedJwt? result
---@return string? error
function sinq.jwt.verify(token, key, algo) end

---@param claims table<string, any>
---@param key string A raw key for `HS*`, a PEM private key for `RS*`, `ES*` and `EdDSA`.
---@param method? string "HS256" by default.
---@return string? result
---@return string? error
function sinq.jwt.sign(claims, key, method) end

sinq.fake = {}

---Seeds the generator, for repeatable output.
---@param seed integer
function sinq.fake.setSeed(seed) end

---@return string
function sinq.fake.uuid() end

---@return string
function sinq.fake.uuidv4() end

---@return string
function sinq.fake.ipv4() end

---@return string
function sinq.fake.ipv6() end

---@return string
function sinq.fake.url() end

---@return string
function sinq.fake.userAgent() end

---@return string traceparent A W3C traceparent header value.
function sinq.fake.trace() end

---@return string
function sinq.fake.email() end

---@return string
function sinq.fake.phone() end

---@return string
function sinq.fake.name() end

---@return string
function sinq.fake.firstName() end

---@return string
function sinq.fake.lastName() end

---@return string
function sinq.fake.username() end

---@return string
function sinq.fake.password() end

---@return string
function sinq.fake.word() end

---@return string
function sinq.fake.address() end

---@return string
function sinq.fake.company() end

---@param fromMs integer
---@param toMs? integer The current time by default.
---@return number timestamp UNIX time in milliseconds.
function sinq.fake.timestamp(fromMs, toMs) end

---@param min? integer
---@param max? integer
---@return integer
function sinq.fake.int(min, max) end

---@param min? number
---@param max? number
---@return number
function sinq.fake.float(min, max) end

---Returns a random boolean.
---@return boolean
function sinq.fake.shakespeare() end

---Returns a random element of an array.
---@generic T
---@param array T[]
---@return T
function sinq.fake.oneOf(array) end

---One match: `full` is the whole match, `[1]`, `[2]`, ... are the capture groups in order,
---and a named group is also available by its name.
---@class sinq.RegexMatch
---@field full string The whole match.
---@field [integer] string|false A capture group, or `false` if the group didn't take part in the match.
---@field [string] string|false A named capture group, or `false` if it didn't take part in the match.

---A compiled regular expression. Both `re.match(s)` and `re:match(s)` work.
---@class sinq.Regex
---@field match fun(source: string): sinq.RegexMatch? The first match, or nil if there is none. Raises an error if `source` isn't a string.
---@field matchAll fun(source: string): sinq.RegexMatch[] Every non-overlapping match from left to right; empty if there are none. Raises an error if `source` isn't a string.

sinq.regex = {}

---Compiles a regular expression in Go's RE2 syntax: no lookarounds or backreferences, and
---matching always runs in linear time. A group can't be named `full`, and two groups can't
---share a name.
---@param pattern string
---@return sinq.Regex? regex
---@return string? error
function sinq.regex.compile(pattern) end
