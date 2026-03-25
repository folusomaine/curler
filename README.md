# postack

`postack` is a lightweight API client for the terminal. It gives you a small
Postman-like workflow with a project-local YAML file, a terminal UI, and a
scriptable CLI for running saved requests.

## Features

- repo-local `.postack.yaml` config
- interactive Bubble Tea TUI
- `postack init`, `postack list`, and `postack run`
- environment interpolation with `${VAR}`
- auth modes: `none`, `bearer`, `basic`
- body modes: `none`, `json`, `raw`
- JSON pretty-printing for responses
- binary response saving with `--output`

## Install and Build

1. Install Go `1.22+`.
2. Fetch dependencies:

   ```bash
   go mod tidy
   ```

3. Build the binary:

   ```bash
   go build -o postack ./cmd/postack
   ```

## Quick Start

Initialize a new config in your current directory:

```bash
./postack init
```

Open the TUI:

```bash
./postack
```

List saved requests:

```bash
./postack list
```

Run a saved request:

```bash
./postack run default/users
```

## Config Discovery

`postack` looks for `.postack.yaml` in the current directory and then walks up
the directory tree until it finds one.

That means you can run `postack` from nested folders inside a repo and it will
still find the nearest project config.

## Config Schema

`postack` stores everything in a single YAML file:

```yaml
version: 1
active_env: local
environments:
  local:
    BASE_URL: http://localhost:8080
    API_TOKEN: local-demo-token
collections:
  default:
    requests:
      users:
        method: GET
        url: ${BASE_URL}/users
        headers:
          Accept: application/json
        query:
          page: "1"
        auth:
          type: bearer
          token: ${API_TOKEN}
        body:
          mode: none
        timeout: 30s
```

Top-level keys:

- `version`: config schema version, currently `1`
- `active_env`: default environment used by the TUI and `postack run`
- `environments.<env>`: key/value pairs used for `${VAR}` interpolation
- `collections.<collection>.requests.<request>`: saved request definitions

Request keys:

- `method`: HTTP method, defaults to `GET`
- `url`: request URL, supports `${VAR}` placeholders
- `headers`: string map of request headers
- `query`: string map of query params
- `auth`: auth block, defaults to `type: none`
- `body`: body block, defaults to `mode: none`
- `timeout`: Go duration string like `500ms`, `5s`, or `1m`

## Full Example With Multiple Requests

```yaml
version: 1
active_env: local

environments:
  local:
    BASE_URL: http://localhost:8080
    API_TOKEN: local-demo-token
    ADMIN_USER: admin
    ADMIN_PASS: super-secret
  staging:
    BASE_URL: https://api.example.com
    API_TOKEN: staging-demo-token
    ADMIN_USER: staging-admin
    ADMIN_PASS: staging-secret

collections:
  default:
    requests:
      health:
        method: GET
        url: ${BASE_URL}/health
        auth:
          type: none
        timeout: 5s

      users:
        method: GET
        url: ${BASE_URL}/users
        headers:
          Accept: application/json
        query:
          page: "1"
          limit: "25"
        auth:
          type: bearer
          token: ${API_TOKEN}
        timeout: 30s

      create-user:
        method: POST
        url: ${BASE_URL}/users
        headers:
          Accept: application/json
        auth:
          type: bearer
          token: ${API_TOKEN}
        body:
          mode: json
          content: |
            {
              "name": "Ada Lovelace",
              "email": "ada@example.com"
            }
        timeout: 30s

      login:
        method: POST
        url: ${BASE_URL}/login
        headers:
          Content-Type: text/plain
        auth:
          type: none
        body:
          mode: raw
          content: |
            username=demo&password=secret

  admin:
    requests:
      audit-log:
        method: GET
        url: ${BASE_URL}/admin/audit
        auth:
          type: basic
          username: ${ADMIN_USER}
          password: ${ADMIN_PASS}
        query:
          limit: "100"
        timeout: 15s
```

This gives you refs like:

- `default/health`
- `default/users`
- `default/create-user`
- `default/login`
- `admin/audit-log`

## Request Permutations

### No Auth

If your API does not use auth, omit the `auth` block or set it explicitly:

```yaml
auth:
  type: none
```

### Bearer Auth

```yaml
auth:
  type: bearer
  token: ${API_TOKEN}
```

### Basic Auth

```yaml
auth:
  type: basic
  username: ${ADMIN_USER}
  password: ${ADMIN_PASS}
```

### No Body

You can omit `body` entirely or use:

```yaml
body:
  mode: none
```

### JSON Body

```yaml
body:
  mode: json
  content: |
    {
      "name": "Ada",
      "email": "ada@example.com"
    }
```

`body.mode: json` automatically sends `Content-Type: application/json` when a
request does not already define a `Content-Type` header.

### Raw Body

```yaml
body:
  mode: raw
  content: |
    raw request payload here
```

For `raw` bodies, set `Content-Type` yourself when the server expects one:

```yaml
headers:
  Content-Type: text/plain
```

### Headers

```yaml
headers:
  Accept: application/json
  X-Trace-Id: abc-123
```

### Query Params

```yaml
query:
  page: "2"
  limit: "50"
  include_archived: "true"
```

### Environment Interpolation

Any string field in the request can use `${VAR}` placeholders:

- `url`
- `headers`
- `query`
- bearer token
- basic auth username/password
- body content
- timeout

Example:

```yaml
url: ${BASE_URL}/users/${USER_ID}
headers:
  X-Api-Key: ${API_KEY}
```

If a referenced variable is missing from the selected environment, the request
will fail with an interpolation error.

## CLI Commands

### `postack`

Launch the TUI using the nearest `.postack.yaml`.

### `postack init`

Create a starter `.postack.yaml` in the current directory.

### `postack list`

Print every saved request ref in `collection/request` form.

Example output:

```text
default/health
default/users
admin/audit-log
```

### `postack run <collection/request>`

Run a saved request without opening the TUI.

Supported flags:

- `--env <name>`: override the active environment
- `--header "Key: Value"`: repeatable header override
- `--header "Key=Value"`: also accepted
- `--query "key=value"`: repeatable query override
- `--body '<raw body>'`: replace the saved body content
- `--timeout 5s`: override timeout
- `--output ./response.bin`: write the response body to a file
- `--status-only`: print only the HTTP response status code

You can place the request ref before or after the flags:

```bash
./postack run default/users --env staging
./postack run --env staging default/users
```

CLI examples:

```bash
./postack run default/health
./postack run default/users --env staging
./postack run default/users --header "X-Debug: 1" --header "X-Trace-Id: abc123"
./postack run default/users --query "page=2" --query "limit=50"
./postack run default/create-user --body '{"name":"Grace"}'
./postack run default/create-user --timeout 10s
./postack run default/users --output ./users.json
./postack run default/health --status-only
```

### `postack run all`

Run every saved request in `.postack.yaml` and print a summary table. Endpoint
failures are reported inline and do not cause the overall command to fail.

Example output:

```text
ENDPOINT         RESULT
admin/audit-log  200
default/health   204
default/users    error: dial tcp timeout
```

CLI override precedence is:

1. CLI flags
2. selected environment interpolation
3. saved request definition

Notes:

- `--body` replaces the saved body content.
- If the saved request has `body.mode: none`, `--body` changes it to `raw`.
- `--output` writes any response body to disk and is especially useful for
  binary responses.

## TUI Usage

Running `./postack` opens the terminal UI.

Layout:

- left pane: saved request list
- upper-right pane: request summary
- lower-right pane: response viewer

Key bindings:

- `q` or `Ctrl+C`: quit
- `Up` / `Down`: move through saved requests
- `Enter` or `Ctrl+R`: run the selected request
- `Tab`: focus the response pane
- `Esc`: move focus back to the request list
- response pane: `Up`, `Down`, `PgUp`, `PgDn`, or mouse wheel to scroll

Requests are read-only in the TUI. Edit `.postack.yaml` to change requests.

## Response Rendering

Every response includes:

- status
- duration
- body size
- response headers

Body rendering behavior:

- JSON is pretty-printed
- non-JSON text is shown as raw text
- binary bodies are omitted from terminal output

For binary or large responses, use:

```bash
./postack run default/users --output ./response.bin
```

## Notes

- `.postack.yaml` can contain secrets in plaintext.
- Starter configs created by `postack init` include demo values for `local` and
  `staging`.
- Request refs must be written as `collection/request`.
