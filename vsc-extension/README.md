# dot-http for VS Code

An extension that behaves like a REST client. When you create a `.http` or `.rest` file, you can send HTTP requests and view the responses directly within the editor.

This project includes two components:

- **VSCode extension** — run `.http` files directly inside VS Code (this)
- **CLI tool** (`dot-http`) — run `.http` files from the terminal (see [dot-http](https://github.com/oktalz/dot-http))

## Features

- **Send HTTP Requests** — execute requests defined in `.http` or `.rest` files via a CodeLens button
- **Variables** — file variables (`@var = value`), `.env` files, system environment variables
- **Multiple environments** — switch env files per file using `@env`
- **Request chaining** — use response data from one request in another
- **Assertions** — assert status codes and response body/header values
- **Cookie sessions** — persist cookies across requests using `@session`
- **GraphQL** — automatic query wrapping
- **Redirect control** — per-request opt-out with `# @no-follow`
- **Importing other files** — share requests and variables across `.http` files
- **Autocomplete** — context-aware completion for directives, `{{variables}}`, request names, HTTP methods, and headers
- **Hover** — hover a `{{variable}}` to see its resolved value/source, or a directive for its docs
- **Diagnostics** — warns on undefined variables, `@requires` pointing at a missing `@name`, duplicate names, and malformed `@assert`
- **Outline & navigation** — named requests in the outline/breadcrumbs; go-to-definition and find-references for `@requires`/`@name` and `{{variables}}`
- **Folding** — collapse request blocks; "Fold All / Unfold All Requests" commands
- **Syntax highlighting** — methods, headers, directives, and `{{variables}}` are colorized

## Usage Examples

### Basic Request
```http
GET https://jsonplaceholder.typicode.com/posts/1
```

### Variables
```http
@baseUrl = https://jsonplaceholder.typicode.com
@postId = 1

GET {{baseUrl}}/posts/{{postId}}
```

Variables can also come from a `.env` file in your workspace root or system environment:
```http
GET {{baseUrl}}/posts?user={{$env USERNAME}}
```

### Multiple Environments

Add `@env` at the top of your file to load a named env file:

```http
@env = prod

###
GET {{API_URL}}/users
```

This loads `prod.env` from the workspace root instead of `.env`.

### Request Chaining
```http
# @name = getPost
GET https://jsonplaceholder.typicode.com/posts/1

###

# @requires = getPost
POST https://jsonplaceholder.typicode.com/posts
Content-Type: application/json

{
    "title": "New Post",
    "userId": {{getPost.body.userId}}
}
```

Available response fields:

- `{{name.body.field}}` — JSON body field (dot notation)
- `{{name.headers.Content-Type}}` — response header

### Assertions

Use `# @expect` to assert the status code and `# @assert` for body/header checks. Results are shown in the response panel.

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

### Cookie Sessions

Add `@session` at the top of your file to persist cookies across requests:

```http
@session = session.json

###
# @name = login
POST https://api.example.com/auth
Content-Type: application/json

{"username": "user", "password": "pass"}

###
# @requires = login
GET https://api.example.com/profile
```

The session file is saved relative to the workspace root.

### GraphQL

Set `Content-Type: application/graphql` — the query is automatically wrapped as `{"query": "..."}` JSON:

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

### Redirect Control

By default redirects are followed (configurable via `dot-http.followRedirects`). To disable for a specific request:

```http
# @no-follow
GET https://api.example.com/redirect
```

### OAuth2

Declare OAuth2 directives as per-request comments. The token is acquired automatically and injected as `Authorization: Bearer <token>`. Tokens are cached with expiry.

#### Client Credentials

```http
# @oauth2-grant = client_credentials
# @oauth2-token-url = https://auth.example.com/oauth/token
# @oauth2-client-id = {{CLIENT_ID}}
# @oauth2-client-secret = {{CLIENT_SECRET}}
# @oauth2-scope = read write
GET https://api.example.com/resource
```

#### Authorization Code + PKCE (browser flow)

Opens a browser tab and captures the callback on a local port.

```http
# @oauth2-grant = authorization_code
# @oauth2-token-url = https://auth.example.com/oauth/token
# @oauth2-auth-url = https://auth.example.com/oauth/authorize
# @oauth2-client-id = {{CLIENT_ID}}
# @oauth2-scope = openid profile
# @oauth2-redirect-port = 9876
GET https://api.example.com/resource
```

#### Device Code Flow

Shows a notification with the verification URL and user code, then polls until approved.

```http
# @oauth2-grant = device_code
# @oauth2-token-url = https://auth.example.com/oauth/token
# @oauth2-device-url = https://auth.example.com/oauth/device/code
# @oauth2-client-id = {{CLIENT_ID}}
# @oauth2-scope = read
GET https://api.example.com/resource
```

#### Token caching

Add `@oauth2-cache` at the file level to persist tokens to a specific file. Otherwise tokens are cached to the file configured in `dot-http.oauth2CacheFile` (default `.tokens.json`).

```http
@oauth2-cache = .tokens.json
```

### Importing Other Files

```http
@import = auth.http

###
# @requires = login
GET https://api.example.com/profile
Authorization: Bearer {{login.body.token}}
```

- Paths are relative to the importing file's directory
- Imports are recursive; cyclic imports are safely skipped

### Autocomplete

The extension offers context-aware completions in `.http` and `.rest` files (trigger automatically as you type, or with `Ctrl+Space`):

- **Directives** — typing `@` suggests `@name`, `@requires`, `@expect`, `@assert`, `@env`, `@session`, `@import`, `@no-follow`, and all `@oauth2-*` directives
- **Variables** — inside `{{ }}` (and `{{$env }}`) suggests variables from `@var` declarations, `.env` files, imported files, and named requests
- **Request names** — after `@requires =`, suggests names of `@name`-declared requests in the file
- **Assertions** — after `@assert`, suggests targets (`status`, `body.`, `header.`) then operators
- **Enums** — `@oauth2-grant =` suggests the grant types; `Content-Type:` suggests common content types
- **Methods & headers** — at the start of a line, suggests HTTP methods (`GET`, `POST`, …) and common header names

### Sending Files (Multipart/Form-Data)

```http
POST https://httpbin.org/post
Content-Type: multipart/form-data; boundary=myBoundary

--myBoundary
Content-Disposition: form-data; name="file1"; filename="README.md"
Content-Type: text/markdown

< ./README.md
--myBoundary--
```

## Extension Settings

| Setting | Default | Description |
| --- | --- | --- |
| `dot-http.responseViewMode` | `reuseTab` | Where to show the response: `reuseTab`, `newTab`, `output` |
| `dot-http.timeout` | _(none)_ | Request timeout, e.g. `30s`, `1m`, `500ms`. Empty = no timeout |
| `dot-http.followRedirects` | `true` | Automatically follow HTTP redirects |
| `dot-http.oauth2CacheFile` | `.tokens.json` | Default token cache file (relative to workspace root) |

## Release Notes

### 0.9.5

Added autocomplete, hover, diagnostics, outline, go-to-definition/references, folding (with Fold/Unfold All commands), and syntax highlighting for `.http` files

### 0.9.4

Added aliasing support for `@requires` dependencies

### 0.9.3

Added expect, assert

### 0.9.0

Added environments, assertions, cookie sessions, GraphQL support, redirect control, timeout setting.

### 0.1.0

Initial release with variable handling, request chaining, and imports.
