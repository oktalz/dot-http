# dot-http

A CLI tool for executing `.http` files — the same format used by the [dot-http VSCode extension](vsc-extension/).

This project includes two components:

- **CLI tool** (`dot-http`) — run `.http` files from the terminal
- **VSCode extension** — run `.http` files directly inside VS Code (see [vsc-extension/](vsc-extension/))

## Install

```bash
go install github.com/oktalz/dot-http
```

## Usage

```bash
dot-http [-H|-B] <file.http> [request-name]
```

- **`-H`** — print response headers only (no body)
- **`-B`** — print response body only (no headers)
- **`file.http`** — path to an `.http` file
- **`request-name`** — (optional) name of the request to run; defaults to the last block in the file

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

**Environment variables** — loaded from a `.env` file in the same directory as the `.http` file:

```env
API_URL=https://api.example.com
TOKEN="secret"
```

```http
GET {{API_URL}}/users
Authorization: Bearer {{TOKEN}}
```

System environment variables are also available. Precedence (highest to lowest):

1. `.env` file
2. System environment variables
3. File variables (`@var = value`)

Use `{{$env VAR}}` or `{{VAR}}` — both resolve through the same lookup.

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

Where `auth.http` contains:

```http
# @name = login
POST https://api.example.com/auth
Content-Type: application/json

{"username": "user", "password": "pass"}
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

# Run a request that chains off others
dot-http api.http create_post

# Headers only
dot-http -H api.http login

# Body only (useful for piping to jq, etc.)
dot-http -B api.http login
```

Output includes the full HTTP response (status, headers, body) with JSON pretty-printing. Use `-H` or `-B` to narrow the output.
