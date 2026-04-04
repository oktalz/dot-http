# dot-http for VS Code

An extension that behaves like a REST client. When you create a `.http` or `.rest` file, you can send HTTP requests and view the responses directly within the editor.

This project includes two components:

- **VSCode extension** — run `.http` files directly inside VS Code (this)
- **CLI tool** (`dot-http`) — run `.http` files from the terminal (see [dot-http](https://github.com/oktalz/dot-http))

## Features

- **Send HTTP Requests**: Execute requests defined in `.http` or `.rest` files.
- **Variables**: Support for file variables (`@variableName = value`) and environment variables (via `.env` files).
- **Request Chaining**: Use the response from one request as input for another.
- **Multipart Form Data**: Send files and form data easily.

## Usage Examples

### Basic Request
```http
GET https://jsonplaceholder.typicode.com/posts/1
```

### Variables
You can define variables at the top of your file and use them in your requests:
```http
@baseUrl = https://jsonplaceholder.typicode.com
@postId = 1

GET {{baseUrl}}/posts/{{postId}}
```
Variables can also come from a `.env` file in your workspace, or system environment variables:
```http
GET {{baseUrl}}/posts?user={{$env USERNAME}}
```

### Request Chaining
You can name requests and use their responses in subsequent requests.

```http
### Get a post
# @name = getPost
GET https://jsonplaceholder.typicode.com/posts/1

### Use the post's userId to create a new post
# @requires = getPost
POST https://jsonplaceholder.typicode.com/posts
Content-Type: application/json

{
    "title": "New Post",
    "body": "This is a new post",
    "userId": {{getPost.body.userId}}
}
```

### Importing Other Files

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

### Sending Files (Multipart/Form-Data)
You can upload files by using the `<` operator to include file contents.

```http
POST https://httpbin.org/post
Content-Type: multipart/form-data; boundary=myBoundary

--myBoundary
Content-Disposition: form-data; name="file1"; filename="README.md"
Content-Type: text/markdown

< ./README.md
--myBoundary
Content-Disposition: form-data; name="file2"; filename="AGENTS.md"
Content-Type: text/markdown

< ./AGENTS.md
--myBoundary--
```

## Extension Settings

This extension contributes the following settings:

* `dot-http.responseViewMode`: Configure where to display the response.
  * `reuseTab` (Default): Updates a single tab with the response content.
  * `newTab`: Opens a new tab for every response.
  * `output`: Displays response in the "Http Client" Output Channel.

## Release Notes

### 1.0.0
Initial release with variable handling, request chaining, and settings.
