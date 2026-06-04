# Change Log

All notable changes to the "http-client" extension will be documented in this file.

Check [Keep a Changelog](http://keepachangelog.com/) for recommendations on how to structure this file.

## 0.9.5

- Added context-aware autocomplete for directives, `{{variables}}`, request names, HTTP methods, headers, assertion targets/operators, OAuth2 grant types, and content types
- Added hover for variables (resolved value/source) and directives (docs)
- Added diagnostics for undefined variables, `@requires` pointing at a missing `@name`, duplicate request names, and malformed `@assert`
- Added document outline, go-to-definition, and find-references for requests and variables
- Added folding for request blocks plus "Fold All / Unfold All Requests" commands
- Added syntax highlighting (TextMate grammar) for methods, headers, directives, and `{{variables}}`

## 0.9.0

- Initial release
