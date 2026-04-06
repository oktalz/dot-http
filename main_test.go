package main

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// parseDocumentRequests
// ---------------------------------------------------------------------------

func TestParseDocumentRequests_SingleBlock(t *testing.T) {
	text := "GET https://example.com/users"
	blocks := parseDocumentRequests(text)

	if len(blocks) != 1 {
		t.Fatalf("expected 1 block, got %d", len(blocks))
	}
	if !strings.Contains(blocks[0].Text, "GET https://example.com/users") {
		t.Errorf("unexpected text: %s", blocks[0].Text)
	}
}

func TestParseDocumentRequests_MultipleBlocks(t *testing.T) {
	text := `GET https://example.com/a
###
POST https://example.com/b
Content-Type: application/json

{"key": "value"}
###
DELETE https://example.com/c`

	blocks := parseDocumentRequests(text)
	if len(blocks) != 3 {
		t.Fatalf("expected 3 blocks, got %d", len(blocks))
	}
	if !strings.Contains(blocks[0].Text, "GET") {
		t.Error("block 0 should be GET")
	}
	if !strings.Contains(blocks[1].Text, "POST") {
		t.Error("block 1 should be POST")
	}
	if !strings.Contains(blocks[2].Text, "DELETE") {
		t.Error("block 2 should be DELETE")
	}
}

func TestParseDocumentRequests_NameMetadata(t *testing.T) {
	text := `# @name = login
POST https://example.com/auth`

	blocks := parseDocumentRequests(text)
	if len(blocks) != 1 {
		t.Fatalf("expected 1 block, got %d", len(blocks))
	}
	if blocks[0].Name != "login" {
		t.Errorf("expected name 'login', got '%s'", blocks[0].Name)
	}
}

func TestParseDocumentRequests_RequiresMetadata(t *testing.T) {
	text := `# @name = profile
# @requires = login
GET https://example.com/profile`

	blocks := parseDocumentRequests(text)
	if len(blocks) != 1 {
		t.Fatalf("expected 1 block, got %d", len(blocks))
	}
	if len(blocks[0].Requires) != 1 {
		t.Fatalf("expected 1 dependency, got %d", len(blocks[0].Requires))
	}
	if blocks[0].Requires[0].Name != "login" {
		t.Errorf("expected dependency 'login', got '%s'", blocks[0].Requires[0].Name)
	}
}

func TestParseDocumentRequests_ParameterizedRequires(t *testing.T) {
	text := `# @name = compare
# @requires = getUser(id=1, name=alice)
GET https://example.com/compare`

	blocks := parseDocumentRequests(text)
	if len(blocks) != 1 {
		t.Fatalf("expected 1 block, got %d", len(blocks))
	}
	dep := blocks[0].Requires[0]
	if dep.Name != "getUser" {
		t.Errorf("expected dep name 'getUser', got '%s'", dep.Name)
	}
	if dep.Args["id"] != "1" {
		t.Errorf("expected arg id=1, got '%s'", dep.Args["id"])
	}
	if dep.Args["name"] != "alice" {
		t.Errorf("expected arg name=alice, got '%s'", dep.Args["name"])
	}
}

func TestParseDocumentRequests_MultipleRequires(t *testing.T) {
	text := `# @name = final
# @requires = first
# @requires = second
GET https://example.com/final`

	blocks := parseDocumentRequests(text)
	if len(blocks[0].Requires) != 2 {
		t.Fatalf("expected 2 dependencies, got %d", len(blocks[0].Requires))
	}
	if blocks[0].Requires[0].Name != "first" {
		t.Errorf("expected 'first', got '%s'", blocks[0].Requires[0].Name)
	}
	if blocks[0].Requires[1].Name != "second" {
		t.Errorf("expected 'second', got '%s'", blocks[0].Requires[1].Name)
	}
}

func TestParseDocumentRequests_EmptyBlocksSkipped(t *testing.T) {
	text := `###
###
GET https://example.com
###
###`

	blocks := parseDocumentRequests(text)
	if len(blocks) != 1 {
		t.Fatalf("expected 1 block (empty ones skipped), got %d", len(blocks))
	}
}

// ---------------------------------------------------------------------------
// parseFileVariables
// ---------------------------------------------------------------------------

func TestParseFileVariables_Basic(t *testing.T) {
	text := `@URL = https://api.example.com
@token = 12345

###
GET {{URL}}/users`

	vars := parseFileVariables(text)
	if vars["URL"] != "https://api.example.com" {
		t.Errorf("expected URL, got '%s'", vars["URL"])
	}
	if vars["token"] != "12345" {
		t.Errorf("expected token '12345', got '%s'", vars["token"])
	}
}

func TestParseFileVariables_IgnoresComments(t *testing.T) {
	text := `# @URL = https://api.example.com
// @token = 12345
@valid = true`

	vars := parseFileVariables(text)
	if _, ok := vars["URL"]; ok {
		t.Error("should not parse commented variable @URL")
	}
	if vars["valid"] != "true" {
		t.Errorf("expected 'true', got '%s'", vars["valid"])
	}
}

func TestParseFileVariables_TrimWhitespace(t *testing.T) {
	text := `  @host  =  localhost:8080  `
	vars := parseFileVariables(text)
	if vars["host"] != "localhost:8080" {
		t.Errorf("expected 'localhost:8080', got '%s'", vars["host"])
	}
}

// ---------------------------------------------------------------------------
// substituteVariables
// ---------------------------------------------------------------------------

func TestSubstituteVariables_Basic(t *testing.T) {
	vars := map[string]any{
		"URL":   "https://api.example.com",
		"token": "12345",
	}
	result := substituteVariables("GET {{URL}}/users?token={{token}}", vars)
	expected := "GET https://api.example.com/users?token=12345"
	if result != expected {
		t.Errorf("expected %q, got %q", expected, result)
	}
}

func TestSubstituteVariables_EnvSyntax(t *testing.T) {
	vars := map[string]any{
		"USERNAME": "zlatko",
	}
	result := substituteVariables("User: {{$env USERNAME}}", vars)
	if result != "User: zlatko" {
		t.Errorf("expected 'User: zlatko', got %q", result)
	}
}

func TestSubstituteVariables_DotNotation(t *testing.T) {
	vars := map[string]any{
		"login": map[string]any{
			"body": map[string]any{
				"token": "abc-123",
				"user": map[string]any{
					"id": float64(10),
				},
			},
			"headers": map[string]any{
				"Content-Type": "application/json",
			},
		},
	}

	tests := []struct {
		input    string
		expected string
	}{
		{"Bearer {{login.body.token}}", "Bearer abc-123"},
		{"User ID: {{login.body.user.id}}", "User ID: 10"},
		{"Type: {{login.headers.Content-Type}}", "Type: application/json"},
	}

	for _, tt := range tests {
		result := substituteVariables(tt.input, vars)
		if result != tt.expected {
			t.Errorf("input %q: expected %q, got %q", tt.input, tt.expected, result)
		}
	}
}

func TestSubstituteVariables_InvalidPathKeepsOriginal(t *testing.T) {
	vars := map[string]any{
		"login": map[string]any{
			"body": map[string]any{},
		},
	}
	result := substituteVariables("Token: {{login.body.token}}", vars)
	if result != "Token: {{login.body.token}}" {
		t.Errorf("expected original placeholder, got %q", result)
	}
}

func TestSubstituteVariables_UnknownVariableKeepsOriginal(t *testing.T) {
	vars := map[string]any{
		"URL": "https://api.example.com",
	}
	result := substituteVariables("GET {{URL}}/users?token={{token}}", vars)
	expected := "GET https://api.example.com/users?token={{token}}"
	if result != expected {
		t.Errorf("expected %q, got %q", expected, result)
	}
}

func TestSubstituteVariables_Precedence(t *testing.T) {
	// Simulates: file vars < system vars < env vars
	fileVars := map[string]any{"HOST": "localhost"}
	sysVars := map[string]any{"HOST": "system-host", "USER": "system-user"}
	envVars := map[string]any{"HOST": "env-host"}

	combined := make(map[string]any)
	for k, v := range fileVars {
		combined[k] = v
	}
	for k, v := range sysVars {
		combined[k] = v
	}
	for k, v := range envVars {
		combined[k] = v
	}

	result := substituteVariables("Host: {{HOST}}, User: {{USER}}", combined)
	expected := "Host: env-host, User: system-user"
	if result != expected {
		t.Errorf("expected %q, got %q", expected, result)
	}
}

// ---------------------------------------------------------------------------
// loadEnvFile
// ---------------------------------------------------------------------------

func TestLoadEnvFile_Basic(t *testing.T) {
	dir := t.TempDir()
	envContent := `URL=https://api.example.com
TOKEN=secret123
# This is a comment
QUOTED_VAL="hello world"
SINGLE_QUOTED='hi there'

EMPTY_LINE_ABOVE=yes`

	os.WriteFile(filepath.Join(dir, ".env"), []byte(envContent), 0644)

	vars := loadEnvFile(dir, "")

	tests := map[string]string{
		"URL":              "https://api.example.com",
		"TOKEN":            "secret123",
		"QUOTED_VAL":       "hello world",
		"SINGLE_QUOTED":    "hi there",
		"EMPTY_LINE_ABOVE": "yes",
	}

	for k, expected := range tests {
		if vars[k] != expected {
			t.Errorf("key %s: expected %q, got %q", k, expected, vars[k])
		}
	}

	// Comments should not appear
	for k := range vars {
		if strings.HasPrefix(k, "#") {
			t.Errorf("comment parsed as variable: %s", k)
		}
	}
}

func TestLoadEnvFile_Missing(t *testing.T) {
	vars := loadEnvFile(t.TempDir(), "")
	if len(vars) != 0 {
		t.Errorf("expected empty map for missing .env, got %d entries", len(vars))
	}
}

// ---------------------------------------------------------------------------
// parseRequest
// ---------------------------------------------------------------------------

func TestParseRequest_GET(t *testing.T) {
	text := "GET https://example.com/users"
	method, url, headers, body, ok := parseRequest(text)

	if !ok {
		t.Fatal("expected ok")
	}
	if method != "GET" {
		t.Errorf("expected GET, got %s", method)
	}
	if url != "https://example.com/users" {
		t.Errorf("unexpected url: %s", url)
	}
	if len(headers) != 0 {
		t.Errorf("expected no headers, got %d", len(headers))
	}
	if body != "" {
		t.Errorf("expected empty body, got %q", body)
	}
}

func TestParseRequest_POSTWithHeadersAndBody(t *testing.T) {
	text := `POST https://example.com/users
Content-Type: application/json
Authorization: Bearer token123

{
    "name": "John",
    "email": "john@example.com"
}`

	method, url, headers, body, ok := parseRequest(text)
	if !ok {
		t.Fatal("expected ok")
	}
	if method != "POST" {
		t.Errorf("expected POST, got %s", method)
	}
	if url != "https://example.com/users" {
		t.Errorf("unexpected url: %s", url)
	}
	if headers["Content-Type"] != "application/json" {
		t.Errorf("missing Content-Type header")
	}
	if headers["Authorization"] != "Bearer token123" {
		t.Errorf("missing Authorization header")
	}
	if !strings.Contains(body, `"name": "John"`) {
		t.Errorf("body should contain name field, got %q", body)
	}
}

func TestParseRequest_SkipsComments(t *testing.T) {
	text := `# This is a comment
// Another comment
# @name = test
GET https://example.com`

	method, url, _, _, ok := parseRequest(text)
	if !ok {
		t.Fatal("expected ok")
	}
	if method != "GET" {
		t.Errorf("expected GET, got %s", method)
	}
	if url != "https://example.com" {
		t.Errorf("unexpected url: %s", url)
	}
}

func TestParseRequest_AllMethods(t *testing.T) {
	methods := []string{"GET", "POST", "PUT", "DELETE", "PATCH", "HEAD", "OPTIONS", "CONNECT", "TRACE"}
	for _, m := range methods {
		text := fmt.Sprintf("%s https://example.com", m)
		method, _, _, _, ok := parseRequest(text)
		if !ok {
			t.Errorf("method %s: expected ok", m)
			continue
		}
		if method != m {
			t.Errorf("expected %s, got %s", m, method)
		}
	}
}

func TestParseRequest_InvalidFormat(t *testing.T) {
	_, _, _, _, ok := parseRequest("# just a comment")
	if ok {
		t.Error("expected not ok for comment-only text")
	}

	_, _, _, _, ok = parseRequest("")
	if ok {
		t.Error("expected not ok for empty text")
	}

	_, _, _, _, ok = parseRequest("INVALIDLINE")
	if ok {
		t.Error("expected not ok for single-word line")
	}
}

// ---------------------------------------------------------------------------
// executeRequestChain
// ---------------------------------------------------------------------------

func mockPerformer(responses map[string]*ResponseData) func(string) (*ResponseData, error) {
	return func(text string) (*ResponseData, error) {
		for key, resp := range responses {
			if strings.Contains(text, key) {
				return resp, nil
			}
		}
		return nil, fmt.Errorf("no mock for request: %s", text)
	}
}

func TestExecuteChain_RecursiveDependencies(t *testing.T) {
	var order []string

	blockC := RequestBlock{Name: "C", Text: "GET https://c.test"}
	blockB := RequestBlock{Name: "B", Requires: []Dependency{{Name: "C", Args: map[string]string{}}}, Text: "GET https://b.test"}
	blockA := RequestBlock{Name: "A", Requires: []Dependency{{Name: "B", Args: map[string]string{}}}, Text: "GET https://a.test"}

	allBlocks := []RequestBlock{blockA, blockB, blockC}
	context := make(map[string]any)

	performer := func(text string) (*ResponseData, error) {
		switch {
		case strings.Contains(text, "c.test"):
			order = append(order, "C")
			return &ResponseData{StatusCode: 200, StatusMessage: "OK", Headers: http.Header{}, Body: `{"val": "fromC"}`}, nil
		case strings.Contains(text, "b.test"):
			order = append(order, "B")
			return &ResponseData{StatusCode: 200, StatusMessage: "OK", Headers: http.Header{}, Body: `{"val": "fromB"}`}, nil
		case strings.Contains(text, "a.test"):
			order = append(order, "A")
			return &ResponseData{StatusCode: 200, StatusMessage: "OK", Headers: http.Header{}, Body: `{"val": "fromA"}`}, nil
		}
		return nil, fmt.Errorf("unknown request")
	}

	_, err := executeRequestChain(&blockA, allBlocks, context, performer, make(map[string]bool), &[]TestResult{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	expected := []string{"C", "B", "A"}
	if len(order) != len(expected) {
		t.Fatalf("expected order %v, got %v", expected, order)
	}
	for i, v := range expected {
		if order[i] != v {
			t.Errorf("position %d: expected %s, got %s", i, v, order[i])
		}
	}

	// Verify context stores responses
	cResp, ok := context["C"].(map[string]any)
	if !ok {
		t.Fatal("context['C'] should be a map")
	}
	cBody, ok := cResp["body"].(map[string]any)
	if !ok {
		t.Fatal("context['C']['body'] should be a map")
	}
	if cBody["val"] != "fromC" {
		t.Errorf("expected 'fromC', got %v", cBody["val"])
	}
}

func TestExecuteChain_CircularDependency(t *testing.T) {
	blockA := RequestBlock{Name: "A", Requires: []Dependency{{Name: "B", Args: map[string]string{}}}, Text: "GET https://a.test"}
	blockB := RequestBlock{Name: "B", Requires: []Dependency{{Name: "A", Args: map[string]string{}}}, Text: "GET https://b.test"}

	allBlocks := []RequestBlock{blockA, blockB}
	context := make(map[string]any)
	performer := func(text string) (*ResponseData, error) {
		return &ResponseData{StatusCode: 200, StatusMessage: "OK", Headers: http.Header{}, Body: `{}`}, nil
	}

	_, err := executeRequestChain(&blockA, allBlocks, context, performer, make(map[string]bool), &[]TestResult{})
	if err == nil {
		t.Fatal("expected circular dependency error")
	}
	if !strings.Contains(err.Error(), "ircular dependency") {
		t.Errorf("expected circular dependency error, got: %v", err)
	}
}

func TestExecuteChain_DependencyNotFound(t *testing.T) {
	blockA := RequestBlock{Name: "A", Requires: []Dependency{{Name: "missing", Args: map[string]string{}}}, Text: "GET https://a.test"}

	allBlocks := []RequestBlock{blockA}
	context := make(map[string]any)
	performer := func(text string) (*ResponseData, error) {
		return &ResponseData{StatusCode: 200, StatusMessage: "OK", Headers: http.Header{}, Body: `{}`}, nil
	}

	_, err := executeRequestChain(&blockA, allBlocks, context, performer, make(map[string]bool), &[]TestResult{})
	if err == nil {
		t.Fatal("expected dependency not found error")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("expected 'not found' error, got: %v", err)
	}
}

func TestExecuteChain_CachesDependencies(t *testing.T) {
	var order []string

	blockC := RequestBlock{Name: "C", Text: "GET https://c.test"}
	blockB := RequestBlock{Name: "B", Requires: []Dependency{{Name: "C", Args: map[string]string{}}}, Text: "GET https://b.test"}
	blockA := RequestBlock{Name: "A", Requires: []Dependency{{Name: "B", Args: map[string]string{}}}, Text: "GET https://a.test"}

	allBlocks := []RequestBlock{blockA, blockB, blockC}
	context := make(map[string]any)

	performer := func(text string) (*ResponseData, error) {
		switch {
		case strings.Contains(text, "c.test"):
			order = append(order, "C")
			return &ResponseData{StatusCode: 200, StatusMessage: "OK", Headers: http.Header{}, Body: `{}`}, nil
		case strings.Contains(text, "b.test"):
			order = append(order, "B")
			return &ResponseData{StatusCode: 200, StatusMessage: "OK", Headers: http.Header{}, Body: `{}`}, nil
		case strings.Contains(text, "a.test"):
			order = append(order, "A")
			return &ResponseData{StatusCode: 200, StatusMessage: "OK", Headers: http.Header{}, Body: `{}`}, nil
		}
		return nil, fmt.Errorf("unknown")
	}

	// First run: all execute
	_, err := executeRequestChain(&blockA, allBlocks, context, performer, make(map[string]bool), &[]TestResult{})
	if err != nil {
		t.Fatal(err)
	}
	if len(order) != 3 {
		t.Fatalf("expected 3 executions, got %d: %v", len(order), order)
	}

	// Second run: B and C are cached, only A re-executes
	order = nil
	_, err = executeRequestChain(&blockA, allBlocks, context, performer, make(map[string]bool), &[]TestResult{})
	if err != nil {
		t.Fatal(err)
	}
	expected := []string{"A"}
	if len(order) != len(expected) {
		t.Fatalf("expected %v, got %v", expected, order)
	}
}

func TestExecuteChain_ParameterizedDependencies(t *testing.T) {
	var order []string

	blockB := RequestBlock{Name: "B", Text: "GET https://b.test/{{id}}"}
	blockA := RequestBlock{
		Name: "A",
		Requires: []Dependency{
			{Name: "B", Args: map[string]string{"id": "1"}},
			{Name: "B", Args: map[string]string{"id": "2"}},
		},
		Text: "GET https://a.test",
	}

	allBlocks := []RequestBlock{blockA, blockB}
	context := make(map[string]any)

	performer := func(text string) (*ResponseData, error) {
		if strings.Contains(text, "b.test") {
			if strings.Contains(text, "/1") {
				order = append(order, "B(1)")
			} else if strings.Contains(text, "/2") {
				order = append(order, "B(2)")
			}
			return &ResponseData{StatusCode: 200, StatusMessage: "OK", Headers: http.Header{}, Body: `{"id": "done"}`}, nil
		}
		if strings.Contains(text, "a.test") {
			order = append(order, "A")
			return &ResponseData{StatusCode: 200, StatusMessage: "OK", Headers: http.Header{}, Body: `{}`}, nil
		}
		return nil, fmt.Errorf("unknown")
	}

	_, err := executeRequestChain(&blockA, allBlocks, context, performer, make(map[string]bool), &[]TestResult{})
	if err != nil {
		t.Fatal(err)
	}

	expected := []string{"B(1)", "B(2)", "A"}
	if len(order) != len(expected) {
		t.Fatalf("expected %v, got %v", expected, order)
	}
	for i, v := range expected {
		if order[i] != v {
			t.Errorf("position %d: expected %s, got %s", i, v, order[i])
		}
	}
}

func TestExecuteChain_VariableSubstitutionInRequest(t *testing.T) {
	blockLogin := RequestBlock{Name: "login", Text: "GET https://login.test"}
	blockProfile := RequestBlock{
		Name:     "profile",
		Requires: []Dependency{{Name: "login", Args: map[string]string{}}},
		Text:     "GET https://profile.test/{{login.body.userId}}",
	}

	allBlocks := []RequestBlock{blockLogin, blockProfile}
	context := make(map[string]any)

	var profileURL string
	performer := func(text string) (*ResponseData, error) {
		if strings.Contains(text, "login.test") {
			return &ResponseData{StatusCode: 200, StatusMessage: "OK", Headers: http.Header{}, Body: `{"userId": 42}`}, nil
		}
		if strings.Contains(text, "profile.test") {
			profileURL = text
			return &ResponseData{StatusCode: 200, StatusMessage: "OK", Headers: http.Header{}, Body: `{}`}, nil
		}
		return nil, fmt.Errorf("unknown")
	}

	_, err := executeRequestChain(&blockProfile, allBlocks, context, performer, make(map[string]bool), &[]TestResult{})
	if err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(profileURL, "/42") {
		t.Errorf("expected userId 42 in URL, got %q", profileURL)
	}
}

func TestExecuteChain_ResponseHeadersAccessible(t *testing.T) {
	blockAuth := RequestBlock{Name: "auth", Text: "GET https://auth.test"}
	blockAPI := RequestBlock{
		Name:     "api",
		Requires: []Dependency{{Name: "auth", Args: map[string]string{}}},
		Text:     "GET https://api.test\nX-Token: {{auth.headers.X-Auth-Token}}",
	}

	allBlocks := []RequestBlock{blockAuth, blockAPI}
	context := make(map[string]any)

	var apiRequestText string
	performer := func(text string) (*ResponseData, error) {
		if strings.Contains(text, "auth.test") {
			return &ResponseData{
				StatusCode:    200,
				StatusMessage: "OK",
				Headers:       http.Header{"X-Auth-Token": []string{"secret-token"}},
				Body:          `{}`,
			}, nil
		}
		if strings.Contains(text, "api.test") {
			apiRequestText = text
			return &ResponseData{StatusCode: 200, StatusMessage: "OK", Headers: http.Header{}, Body: `{}`}, nil
		}
		return nil, fmt.Errorf("unknown")
	}

	_, err := executeRequestChain(&blockAPI, allBlocks, context, performer, make(map[string]bool), &[]TestResult{})
	if err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(apiRequestText, "secret-token") {
		t.Errorf("expected header substitution, got %q", apiRequestText)
	}
}

// ---------------------------------------------------------------------------
// loadSystemEnv
// ---------------------------------------------------------------------------

func TestLoadSystemEnv(t *testing.T) {
	os.Setenv("HTTP_CLIENT_TEST_VAR", "test_value_123")
	defer os.Unsetenv("HTTP_CLIENT_TEST_VAR")

	vars := loadSystemEnv()
	if vars["HTTP_CLIENT_TEST_VAR"] != "test_value_123" {
		t.Errorf("expected 'test_value_123', got '%s'", vars["HTTP_CLIENT_TEST_VAR"])
	}
}

// ---------------------------------------------------------------------------
// Full .http file parsing integration
// ---------------------------------------------------------------------------

func TestFullFileParsing(t *testing.T) {
	text := `@baseUrl = https://api.example.com
@token = abc123

###
# @name = login
POST {{baseUrl}}/auth
Content-Type: application/json

{"user": "test"}

###

# @name = getProfile
# @requires = login
GET {{baseUrl}}/profile
Authorization: Bearer {{token}}

###

# @name = updateProfile
# @requires = getProfile
# @requires = login
PUT {{baseUrl}}/profile
Content-Type: application/json

{"name": "updated"}`

	// Test file variable parsing
	vars := parseFileVariables(text)
	if vars["baseUrl"] != "https://api.example.com" {
		t.Errorf("baseUrl: expected 'https://api.example.com', got '%s'", vars["baseUrl"])
	}
	if vars["token"] != "abc123" {
		t.Errorf("token: expected 'abc123', got '%s'", vars["token"])
	}

	// Test block parsing (first block is variable declarations before first ###)
	blocks := parseDocumentRequests(text)
	if len(blocks) != 4 {
		t.Fatalf("expected 4 blocks, got %d", len(blocks))
	}

	// Block 0: variable declarations (no name, no method)
	// Block 1: login
	if blocks[1].Name != "login" {
		t.Errorf("block 1 name: expected 'login', got '%s'", blocks[1].Name)
	}
	if len(blocks[1].Requires) != 0 {
		t.Errorf("block 1 should have no dependencies")
	}

	// Block 2: getProfile
	if blocks[2].Name != "getProfile" {
		t.Errorf("block 2 name: expected 'getProfile', got '%s'", blocks[2].Name)
	}
	if len(blocks[2].Requires) != 1 || blocks[2].Requires[0].Name != "login" {
		t.Errorf("block 2 should require 'login'")
	}

	// Block 3: updateProfile
	if blocks[3].Name != "updateProfile" {
		t.Errorf("block 3 name: expected 'updateProfile', got '%s'", blocks[3].Name)
	}
	if len(blocks[3].Requires) != 2 {
		t.Fatalf("block 3 should have 2 dependencies, got %d", len(blocks[3].Requires))
	}
	if blocks[3].Requires[0].Name != "getProfile" {
		t.Errorf("block 3 dep 0: expected 'getProfile', got '%s'", blocks[3].Requires[0].Name)
	}
	if blocks[3].Requires[1].Name != "login" {
		t.Errorf("block 3 dep 1: expected 'login', got '%s'", blocks[3].Requires[1].Name)
	}

	// Test variable substitution in request text
	context := map[string]any{
		"baseUrl": "https://api.example.com",
		"token":   "abc123",
	}
	resolved := substituteVariables(blocks[2].Text, context)
	if !strings.Contains(resolved, "https://api.example.com/profile") {
		t.Errorf("variable substitution failed in URL: %s", resolved)
	}
	if !strings.Contains(resolved, "Bearer abc123") {
		t.Errorf("variable substitution failed in header: %s", resolved)
	}
}

// ---------------------------------------------------------------------------
// printResponse output modes
// ---------------------------------------------------------------------------

func capturePrintResponse(resp *ResponseData, mode outputMode) string {
	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	printResponse(resp, mode)

	w.Close()
	os.Stdout = old

	var buf strings.Builder
	io.Copy(&buf, r)
	return buf.String()
}

func TestPrintResponse_All(t *testing.T) {
	resp := &ResponseData{
		StatusCode:    200,
		StatusMessage: "200 OK",
		Headers:       http.Header{"Content-Type": {"application/json"}},
		Body:          `{"key":"value"}`,
	}

	out := capturePrintResponse(resp, outputAll)

	if !strings.Contains(out, "200 OK") {
		t.Error("outputAll should contain status line")
	}
	if !strings.Contains(out, "Content-Type: application/json") {
		t.Error("outputAll should contain headers")
	}
	if !strings.Contains(out, `"key"`) {
		t.Error("outputAll should contain body")
	}
}

func TestPrintResponse_HeadersOnly(t *testing.T) {
	resp := &ResponseData{
		StatusCode:    200,
		StatusMessage: "200 OK",
		Headers:       http.Header{"Content-Type": {"application/json"}},
		Body:          `{"key":"value"}`,
	}

	out := capturePrintResponse(resp, outputHeaders)

	if !strings.Contains(out, "200 OK") {
		t.Error("outputHeaders should contain status line")
	}
	if !strings.Contains(out, "Content-Type: application/json") {
		t.Error("outputHeaders should contain headers")
	}
	if strings.Contains(out, `"key"`) {
		t.Error("outputHeaders should NOT contain body")
	}
}

func TestPrintResponse_BodyOnly(t *testing.T) {
	resp := &ResponseData{
		StatusCode:    200,
		StatusMessage: "200 OK",
		Headers:       http.Header{"Content-Type": {"application/json"}},
		Body:          `{"key":"value"}`,
	}

	out := capturePrintResponse(resp, outputBody)

	if strings.Contains(out, "200 OK") {
		t.Error("outputBody should NOT contain status line")
	}
	if strings.Contains(out, "Content-Type") {
		t.Error("outputBody should NOT contain headers")
	}
	if !strings.Contains(out, `"key"`) {
		t.Error("outputBody should contain body")
	}
}

// ---------------------------------------------------------------------------
// parseImports
// ---------------------------------------------------------------------------

func TestParseImports_Basic(t *testing.T) {
	dir := t.TempDir()

	// Create imported file with a named request and a variable
	authContent := `@authUrl = https://auth.example.com

###
# @name = login
POST {{authUrl}}/token
Content-Type: application/json

{"user": "test"}
`
	os.WriteFile(filepath.Join(dir, "auth.http"), []byte(authContent), 0644)

	// Main file imports auth.http
	mainContent := fmt.Sprintf("@import = auth.http\n@baseUrl = https://api.example.com\n\n###\n# @requires = login\nGET {{baseUrl}}/profile\n")

	visited := map[string]bool{filepath.Join(dir, "main.http"): true}
	blocks, vars, err := parseImports(mainContent, dir, visited)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should have imported the login block
	found := false
	for _, b := range blocks {
		if b.Name == "login" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected to find 'login' block from import")
	}

	// Should have imported authUrl variable
	if vars["authUrl"] != "https://auth.example.com" {
		t.Errorf("expected imported variable authUrl, got '%s'", vars["authUrl"])
	}

	// Should have imported blocks (variable block + login block from auth.http)
	if len(blocks) < 1 {
		t.Errorf("expected at least 1 imported block, got %d", len(blocks))
	}
}

func TestParseImports_Multiple(t *testing.T) {
	dir := t.TempDir()

	os.WriteFile(filepath.Join(dir, "a.http"), []byte("# @name = reqA\nGET https://a.test\n"), 0644)
	os.WriteFile(filepath.Join(dir, "b.http"), []byte("# @name = reqB\nGET https://b.test\n"), 0644)

	mainContent := "@import = a.http\n@import = b.http\n"

	visited := map[string]bool{}
	blocks, _, err := parseImports(mainContent, dir, visited)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	names := make(map[string]bool)
	for _, b := range blocks {
		if b.Name != "" {
			names[b.Name] = true
		}
	}
	if !names["reqA"] {
		t.Error("expected reqA from a.http import")
	}
	if !names["reqB"] {
		t.Error("expected reqB from b.http import")
	}
}

func TestParseImports_NestedImports(t *testing.T) {
	dir := t.TempDir()

	// base.http defines a request
	os.WriteFile(filepath.Join(dir, "base.http"), []byte("# @name = base\nGET https://base.test\n"), 0644)

	// mid.http imports base.http
	os.WriteFile(filepath.Join(dir, "mid.http"), []byte("@import = base.http\n\n###\n# @name = mid\nGET https://mid.test\n"), 0644)

	// main imports mid.http — should get both base and mid
	mainContent := "@import = mid.http\n"

	visited := map[string]bool{}
	blocks, _, err := parseImports(mainContent, dir, visited)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	names := make(map[string]bool)
	for _, b := range blocks {
		if b.Name != "" {
			names[b.Name] = true
		}
	}
	if !names["base"] {
		t.Error("expected 'base' from nested import")
	}
	if !names["mid"] {
		t.Error("expected 'mid' from direct import")
	}
}

func TestParseImports_CyclicImportsHandled(t *testing.T) {
	dir := t.TempDir()

	// a.http imports b.http, b.http imports a.http
	os.WriteFile(filepath.Join(dir, "a.http"), []byte("@import = b.http\n\n###\n# @name = reqA\nGET https://a.test\n"), 0644)
	os.WriteFile(filepath.Join(dir, "b.http"), []byte("@import = a.http\n\n###\n# @name = reqB\nGET https://b.test\n"), 0644)

	mainContent := "@import = a.http\n"

	visited := map[string]bool{}
	blocks, _, err := parseImports(mainContent, dir, visited)
	if err != nil {
		t.Fatalf("cyclic import should not error, got: %v", err)
	}

	names := make(map[string]bool)
	for _, b := range blocks {
		if b.Name != "" {
			names[b.Name] = true
		}
	}
	if !names["reqA"] {
		t.Error("expected reqA")
	}
	if !names["reqB"] {
		t.Error("expected reqB")
	}
}

func TestParseImports_MissingFileErrors(t *testing.T) {
	dir := t.TempDir()
	mainContent := "@import = nonexistent.http\n"

	visited := map[string]bool{}
	_, _, err := parseImports(mainContent, dir, visited)
	if err == nil {
		t.Error("expected error for missing import file")
	}
}

func TestParseImports_VariablesMerged(t *testing.T) {
	dir := t.TempDir()

	os.WriteFile(filepath.Join(dir, "vars.http"), []byte("@imported_var = imported_value\n@shared = from_import\n"), 0644)

	mainContent := "@import = vars.http\n@shared = from_main\n"

	visited := map[string]bool{}
	_, importedVars, err := parseImports(mainContent, dir, visited)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if importedVars["imported_var"] != "imported_value" {
		t.Errorf("expected 'imported_value', got '%s'", importedVars["imported_var"])
	}

	// Imported vars only — main file vars override later in the pipeline
	if importedVars["shared"] != "from_import" {
		t.Errorf("expected 'from_import' in imported vars, got '%s'", importedVars["shared"])
	}
}

func TestImport_EndToEnd_ChainAcrossFiles(t *testing.T) {
	dir := t.TempDir()

	// auth.http: a named request
	authContent := `# @name = login
GET https://login.test
`
	os.WriteFile(filepath.Join(dir, "auth.http"), []byte(authContent), 0644)

	// main.http: imports auth.http, uses login as dependency
	mainContent := `@import = auth.http

###
# @name = profile
# @requires = login
GET https://profile.test/{{login.body.userId}}
`
	os.WriteFile(filepath.Join(dir, "main.http"), []byte(mainContent), 0644)

	mainText := mainContent
	baseDir := dir

	absMain := filepath.Join(dir, "main.http")
	visited := map[string]bool{absMain: true}
	importedBlocks, _, err := parseImports(mainText, baseDir, visited)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	localBlocks := parseDocumentRequests(mainText)
	allBlocks := append(importedBlocks, localBlocks...)

	// Find profile block
	var target *RequestBlock
	for i := range allBlocks {
		if allBlocks[i].Name == "profile" {
			target = &allBlocks[i]
			break
		}
	}
	if target == nil {
		t.Fatal("profile block not found")
	}

	var profileURL string
	performer := func(text string) (*ResponseData, error) {
		if strings.Contains(text, "login.test") {
			return &ResponseData{StatusCode: 200, StatusMessage: "OK", Headers: http.Header{}, Body: `{"userId": 99}`}, nil
		}
		if strings.Contains(text, "profile.test") {
			profileURL = text
			return &ResponseData{StatusCode: 200, StatusMessage: "OK", Headers: http.Header{}, Body: `{}`}, nil
		}
		return nil, fmt.Errorf("unknown")
	}

	context := make(map[string]any)
	_, err = executeRequestChain(target, allBlocks, context, performer, make(map[string]bool), &[]TestResult{})
	if err != nil {
		t.Fatalf("chain error: %v", err)
	}

	if !strings.Contains(profileURL, "/99") {
		t.Errorf("expected userId 99 in URL from imported login request, got %q", profileURL)
	}
}
