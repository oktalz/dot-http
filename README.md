# dot-http

A CLI tool for executing `.http` files — the same format used by the [dot-http VSCode extension](vsc-extension/).

This project includes two components:

- **CLI tool** (`dot-http`) — run `.http` files from the terminal
- **VSCode extension** — run `.http` files directly inside VS Code (see [vsc-extension/](vsc-extension/))

## Install

```bash
go install github.com/oktalz/dot-http@latest
```

## Usage

```bash
dot-http [flags] <file.http> [request-name]
```

### Flags

| Flag | Description |
| --- | --- |
| `-v` | Print version and exit |
| `-V` | Verbose: print request details before sending |
| `-H` | Print response headers only (no body) |
| `-B` | Print response body only (no headers) |
| `-o <file>` | Save response body to a file |
| `--env <name>` | Load `<name>.env` instead of `.env` |
| `--timeout <dur>` | Request timeout, e.g. `30s`, `1m` (default: none) |
| `--no-follow` | Do not follow redirects |
| `--session <file>` | Load/save cookies from/to a JSON session file |
| `--junit <file>` | Write JUnit XML test report to file |

## Features

### HTTP methods

All standard methods: GET, POST, PUT, DELETE, PATCH, HEAD, OPTIONS, CONNECT, TRACE.

### .http file format

Requests are separated by `###`. Each block can have headers (key: value lines after the method line) and a body (everything after the first blank line).

```http
GET https://api.example.com/users

###

POST https://api.example.com/users
Content-Type: application/json

{
    "name": "John"
}
```

### Variables

**File variables** — defined at the top of the `.http` file:

```http
@baseUrl = https://api.example.com

###
GET {{baseUrl}}/users
```

**Environment variables** — from system environment and a `.env` file in the current working directory (system env takes precedence):

```env
API_URL=https://api.example.com
TOKEN="secret"
```

```http
GET {{API_URL}}/users
Authorization: Bearer {{TOKEN}}
```

Precedence (highest to lowest):

1. System environment variables
2. `.env` file (or `<name>.env` when using `--env <name>`)
3. File variables (`@var = value`)

Use `{{$env VAR}}` or `{{VAR}}` — both resolve through the same lookup.

### Multiple environments

Use `--env` to load a named environment file:

```bash
dot-http --env prod api.http        # loads prod.env
dot-http --env staging api.http     # loads staging.env
```

### Request chaining

Name a request with `# @name` and reference its response in subsequent requests using dot notation:

```http
# @name = login
POST https://api.example.com/auth
Content-Type: application/json

{"username": "user", "password": "pass"}

###

# @requires = login
GET https://api.example.com/profile
Authorization: Bearer {{login.body.token}}
```

Available response fields:

- `{{name.body.field}}` — JSON response body (dot notation for nested fields)
- `{{name.headers.Content-Type}}` — response headers

### Parameterized dependencies

Pass arguments to dependencies to reuse the same request with different parameters:

```http
# @name = getUser
GET https://api.example.com/users/{{id}}

###

# @requires = getUser(id=1)
# @requires = getUser(id=2)
GET https://api.example.com/compare
```

### Assertions & testing

Use `# @expect` to assert the response status code, and `# @assert` for header/body checks. Exit code is non-zero on failure — suitable for CI.

```http
# @name = createUser
# @expect 201
# @assert body.name == John
# @assert header.Content-Type contains application/json
POST https://api.example.com/users
Content-Type: application/json

{"name": "John"}
```

Supported operators: `==`, `!=`, `contains`, `!contains`

Assertion targets:

- `status` — HTTP status code
- `body.<path>` — JSON body field (dot notation)
- `header.<Name>` — response header value

### JUnit XML reports

Generate a JUnit-compatible XML report for CI/CD systems (GitHub Actions, Jenkins, etc.):

```bash
dot-http --junit report.xml api.http
```

### GraphQL

Set `Content-Type: application/graphql` and write the query as the body — it will be automatically wrapped as `{"query": "..."}` JSON:

```http
POST https://api.example.com/graphql
Content-Type: application/graphql

{
  users {
    id
    name
  }
}
```

### Cookie sessions

Persist cookies between runs using a session file:

```bash
dot-http --session session.json api.http login
dot-http --session session.json api.http profile   # reuses cookies from login
```

### Importing other files

Split requests across files using `@import`. Imported files make their named requests and variables available to the current file.

```http
@import = auth.http
@import = helpers/users.http

###
# @requires = login
GET https://api.example.com/profile
Authorization: Bearer {{login.body.token}}
```

- Paths are relative to the importing file's directory
- Multiple `@import` directives are supported
- Imports are recursive (imported files can import other files)
- Cyclic imports are detected and safely skipped

### Circular dependency detection

The tool detects and reports circular dependencies with an error message.

## Examples

```bash
# Run the last request in the file
dot-http api.http

# Run a specific named request (and its dependencies)
dot-http api.http login

# Print version
dot-http -v

# Verbose: show request details before sending
dot-http -V api.http login

# Headers only
dot-http -H api.http login

# Body only (useful for piping to jq, etc.)
dot-http -B api.http login | jq .

# Save response body to file
dot-http -o response.json api.http

# Use a named environment
dot-http --env prod api.http deploy

# Set a request timeout
dot-http --timeout 10s api.http slow_request

# Do not follow redirects
dot-http --no-follow api.http check_redirect

# Persist cookies across requests
dot-http --session cookies.json api.http login

# Run assertions and output JUnit report
dot-http --junit results.xml api.http create_user
```

Output includes the full HTTP response (status, headers, body) with JSON pretty-printing.
