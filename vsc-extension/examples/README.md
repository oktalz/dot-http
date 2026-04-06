# dot-http Examples

This folder contains comprehensive examples demonstrating all features of the dot-http VSCode extension, from basic to advanced usage.

## Quick Start

1. **Clone or download** this repository to your VSCode workspace
2. **Open** any `.http` file in the examples folder
3. **Click** the "Run Request" CodeLens button above any request block
4. **View** the response in a new tab or the output panel

## Example Files

### Basic Features

#### [01-basic.http](01-basic.http)
Simple HTTP requests with different methods.
- GET, POST, PUT, PATCH, DELETE, HEAD
- Basic JSON request/response
- **No dependencies** — run any request

#### [02-variables.http](02-variables.http)
Variable substitution and environment integration.
- File variables: `@var = value`
- Environment variables: `{{$env VAR}}`
- Variable precedence (system > .env file > file variables)
- **Setup needed:** optional `.env` file with `VAR=value`

#### [03-chaining.http](03-chaining.http)
Request chaining and dependencies.
- `@name` to identify requests
- `@requires` to declare dependencies
- Access response data: `{{name.body.field}}`, `{{name.headers.Header}}`
- Parameterized dependencies: `@requires = name(id=123)`
- **Run in order:** later requests depend on earlier ones

#### [04-assertions.http](04-assertions.http)
Validate status codes and response content.
- `@expect 200` — assert status code
- `@assert body.field == value` — assert body values
- `@assert header.Header contains text` — assert headers
- Operators: `==`, `!=`, `contains`, `!contains`
- **Setup:** just run, results shown in response panel

#### [05-sessions.http](05-sessions.http)
Persist cookies across requests.
- `@session = file.json` — define session file
- Cookies auto-saved after each response
- Cookies auto-loaded in subsequent requests
- Domain-aware: different domains have separate cookies
- **Setup:** add `@session = session.json` at top

#### [06-graphql.http](06-graphql.http)
GraphQL query support with automatic wrapping.
- `Content-Type: application/graphql`
- Query auto-wrapped as `{"query": "..."}`
- Works with variables and chaining
- **No setup needed**

### OAuth2 Authentication

#### [07-oauth2-client-credentials.http](07-oauth2-client-credentials.http)
OAuth2 client credentials grant (machine-to-machine).
- `@oauth2-grant = client_credentials`
- Auto token acquisition and injection
- Token caching with expiry
- **Setup needed:**
  ```bash
  # Add to .env or system environment:
  CLIENT_ID=your-client-id
  CLIENT_SECRET=your-client-secret
  ```

#### [08-oauth2-authcode.http](08-oauth2-authcode.http)
OAuth2 authorization code + PKCE (user login via browser).
- `@oauth2-grant = authorization_code`
- Opens browser for user login
- Automatic PKCE handling
- Token cached and reused
- **Setup needed:**
  - Register your app with OAuth2 provider
  - Set redirect URI to `http://localhost:9876` (or custom port)
  - Add credentials to `.env`

#### [09-oauth2-device-code.http](09-oauth2-device-code.http)
OAuth2 device code flow (for headless devices).
- `@oauth2-grant = device_code`
- Shows verification URL and user code in notification
- User authorizes on another device
- Client polls until authorized
- **Setup needed:** OAuth2 provider with device code support

### Advanced Features

#### [10-multipart-formdata.http](10-multipart-formdata.http)
File uploads with form fields.
- `Content-Type: multipart/form-data`
- File insertion: `< ./path/to/file`
- Mix files and form fields
- Works with OAuth2 and other auth
- **Setup needed:** files must exist locally

#### [11-environments.http](11-environments.http)
Switch between environments without code changes.
- `@env = dev` or `@env = prod`
- Load different `.env` files (dev.env, prod.env, etc.)
- Variable precedence rules
- **Setup needed:**
  ```bash
  # Create environment files:
  .env           # default
  dev.env        # development
  staging.env    # staging
  prod.env       # production (don't commit!)
  ```

#### [12-imports.http](12-imports.http)
Share requests and variables across files.
- `@import = other-file.http`
- Access imported requests via `@requires`
- Reuse variables from imported files
- Recursive and cyclic-safe
- **Setup needed:** create files to import from

#### [13-advanced-full-featured.http](13-advanced-full-featured.http)
**Full integration example** combining multiple features.
- OAuth2 authentication
- Request chaining with assertions
- File uploads
- GraphQL queries
- Session persistence
- Environment switching
- Variable substitution
- **The most complete example** — study this for real-world usage

#### [14-redirect-timeout-no-follow.http](14-redirect-timeout-no-follow.http)
Special features: redirects, timeouts, and redirect control.
- `@no-follow` — disable redirect following for a request
- `dot-http.timeout` setting
- Status code validation after redirects
- **Setup needed:** configure `dot-http.timeout` in VS Code settings

## Setup Instructions

### 1. Environment Variables (.env file)

Create a `.env` file in your workspace root:

```env
# API credentials
CLIENT_ID=your-oauth-client-id
CLIENT_SECRET=your-oauth-client-secret
API_KEY=your-api-key
USERNAME=your-username

# API URLs (for different services)
API_URL=https://api.example.com
GRAPHQL_URL=https://api.example.com/graphql
```

**Note:** Add `.env` to `.gitignore` to avoid committing credentials.

### 2. VS Code Settings

Open VS Code settings and configure dot-http:

```json
{
  "dot-http.timeout": "30s",
  "dot-http.followRedirects": true,
  "dot-http.responseViewMode": "reuseTab"
}
```

Or edit through UI: Settings → Extensions → Http Client

### 3. OAuth2 Configuration

For authorization code flow, ensure your OAuth2 app is configured with redirect URI:
```
http://localhost:9876
```

You can use a custom port with `@oauth2-redirect-port = 8080`.

## Features Summary

| Feature | File | Complexity |
|---------|------|-----------|
| Basic Requests | 01-basic.http | ⭐ |
| Variables | 02-variables.http | ⭐⭐ |
| Chaining | 03-chaining.http | ⭐⭐ |
| Assertions | 04-assertions.http | ⭐⭐ |
| Sessions | 05-sessions.http | ⭐⭐ |
| GraphQL | 06-graphql.http | ⭐⭐ |
| OAuth2 (Client Creds) | 07-oauth2-client-credentials.http | ⭐⭐⭐ |
| OAuth2 (Auth Code) | 08-oauth2-authcode.http | ⭐⭐⭐ |
| OAuth2 (Device Code) | 09-oauth2-device-code.http | ⭐⭐⭐ |
| File Uploads | 10-multipart-formdata.http | ⭐⭐ |
| Environments | 11-environments.http | ⭐⭐ |
| File Imports | 12-imports.http | ⭐⭐⭐ |
| Full Integration | 13-advanced-full-featured.http | ⭐⭐⭐⭐ |
| Redirects & Timeouts | 14-redirect-timeout-no-follow.http | ⭐⭐ |

## Usage Tips

### Running Requests in VS Code

1. **Single Request:** Click "Run Request" above any `GET`/`POST`/etc line
2. **With Dependencies:** Click on a request with `@requires` — dependencies run automatically
3. **View Responses:**
   - New Tab: Response opens in new editor tab
   - Reuse Tab: Response overwrites previous response
   - Output Panel: Response shows in Output panel

### Common Workflows

#### Development & Testing
```
1. Use 01-basic.http to verify API endpoints exist
2. Use 02-variables.http to parameterize requests
3. Use 03-chaining.http to test workflows
4. Use 04-assertions.http to validate responses
```

#### API Integration
```
1. Use 07-oauth2-client-credentials.http for server-to-server
2. Use 08-oauth2-authcode.http for user-facing apps
3. Use 05-sessions.http to test session/cookie handling
4. Use 10-multipart-formdata.http to test file uploads
```

#### Multi-Environment Deployment
```
1. Create dev.env, staging.env, prod.env
2. Use 11-environments.http with @env = dev (then staging, then prod)
3. Share requests via 12-imports.http
4. Run 13-advanced-full-featured.http for full integration test
```

## Troubleshooting

### "Authorization: Bearer undefined"
**Problem:** OAuth2 token not acquired.
**Solution:** 
- Check credentials in `.env` are correct
- Verify OAuth2 provider URL is correct
- Check scope is valid for your OAuth2 app

### "Variable {{VAR}} not found"
**Problem:** Variable not defined.
**Solution:**
- Add to `.env` file
- Or add `@var = value` in the `.http` file
- Check variable name matches (case-sensitive)

### "Connection refused" or timeout
**Problem:** API endpoint unreachable.
**Solution:**
- Verify API URL is correct
- Check network connectivity
- Increase timeout if API is slow: `dot-http.timeout: "60s"`

### "Redirect loop detected"
**Problem:** Infinite redirect chain.
**Solution:**
- Use `@no-follow` to inspect redirects
- Check Location header in response
- Manually follow to correct endpoint

## Performance Notes

- **Token Caching:** OAuth2 tokens are cached to avoid re-authentication
- **Redirect Following:** Automatic redirects can slow requests; use `@no-follow` if not needed
- **Timeout Setting:** Prevents hung requests in slow networks

## Additional Resources

- [dot-http CLI Tool](https://github.com/oktalz/dot-http) — Run `.http` files from terminal
- [HTTP/1.1 Spec](https://tools.ietf.org/html/rfc7231) — HTTP method definitions
- [OAuth2 Spec](https://tools.ietf.org/html/rfc6749) — Authorization framework
- [GraphQL](https://graphql.org/) — Query language for APIs

## Next Steps

1. **Start Simple:** Run 01-basic.http to verify extension works
2. **Add Variables:** Move to 02-variables.http for real APIs
3. **Test Workflows:** Use 03-chaining.http for multi-step flows
4. **Integrate Auth:** Pick OAuth2 example matching your use case
5. **Study Integration:** Review 13-advanced-full-featured.http for complete patterns

Happy testing! 🚀
