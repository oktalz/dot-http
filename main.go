package main

import (
	"bufio"
	"crypto/tls"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/joho/godotenv"
	"github.com/oktalz/dot-http/version"
)

// ResponseData holds the HTTP response.
type ResponseData struct {
	StatusCode    int
	StatusMessage string
	Headers       http.Header
	Body          string
}

// Dependency represents a @requires directive with optional arguments.
type Dependency struct {
	Name string
	Args map[string]string
}

// Assertion represents a # @assert directive.
type Assertion struct {
	Target string // "status", "body.<path>", "header.<name>"
	Op     string // "==", "!=", "contains", "!contains"
	Value  string
}

// RequestBlock represents a single request block in an .http file.
type RequestBlock struct {
	Name         string
	Requires     []Dependency
	Text         string
	ExpectStatus int // 0 = no assertion
	Assertions   []Assertion
}

type outputMode int

const (
	outputAll     outputMode = iota
	outputHeaders            // -H: headers only
	outputBody               // -B: body only
)

// Config holds all CLI flags.
type Config struct {
	mode        outputMode
	outputFile  string
	timeout     time.Duration
	noFollow    bool
	verbose     bool
	sessionFile string
	junitFile   string
	envName     string
}

// TestResult collects assertion results for JUnit reporting.
type TestResult struct {
	Name     string
	Failures []string
}

// JUnit XML structures.
type junitTestSuites struct {
	XMLName xml.Name         `xml:"testsuites"`
	Suites  []junitTestSuite `xml:"testsuite"`
}

type junitTestSuite struct {
	XMLName  xml.Name        `xml:"testsuite"`
	Name     string          `xml:"name,attr"`
	Tests    int             `xml:"tests,attr"`
	Failures int             `xml:"failures,attr"`
	Cases    []junitTestCase `xml:"testcase"`
}

type junitTestCase struct {
	XMLName xml.Name      `xml:"testcase"`
	Name    string        `xml:"name,attr"`
	Failure *junitFailure `xml:"failure,omitempty"`
}

type junitFailure struct {
	Message string `xml:"message,attr"`
	Text    string `xml:",chardata"`
}

// cookieEntry is a serializable cookie for session files.
type cookieEntry struct {
	Name     string `json:"name"`
	Value    string `json:"value"`
	Domain   string `json:"domain"`
	Path     string `json:"path"`
	Secure   bool   `json:"secure"`
	HttpOnly bool   `json:"httpOnly"`
}

// persistentJar wraps a standard cookie jar with JSON file persistence.
type persistentJar struct {
	inner   http.CookieJar
	file    string
	entries map[string][]cookieEntry // hostname -> cookies
}

func newPersistentJar(file string) (*persistentJar, error) {
	inner, err := cookiejar.New(nil)
	if err != nil {
		return nil, err
	}
	pj := &persistentJar{inner: inner, file: file, entries: make(map[string][]cookieEntry)}

	if data, err := os.ReadFile(file); err == nil {
		var loaded map[string][]cookieEntry
		if json.Unmarshal(data, &loaded) == nil {
			pj.entries = loaded
			for hostname, cookies := range loaded {
				u := &url.URL{Scheme: "https", Host: hostname}
				var hc []*http.Cookie
				for _, c := range cookies {
					hc = append(hc, &http.Cookie{Name: c.Name, Value: c.Value, Domain: c.Domain, Path: c.Path, Secure: c.Secure, HttpOnly: c.HttpOnly})
				}
				inner.SetCookies(u, hc)
			}
		}
	}
	return pj, nil
}

func (j *persistentJar) SetCookies(u *url.URL, cookies []*http.Cookie) {
	j.inner.SetCookies(u, cookies)
	host := u.Hostname()
	existing := make(map[string]int)
	for i, e := range j.entries[host] {
		existing[e.Name] = i
	}
	for _, c := range cookies {
		entry := cookieEntry{Name: c.Name, Value: c.Value, Domain: c.Domain, Path: c.Path, Secure: c.Secure, HttpOnly: c.HttpOnly}
		if idx, ok := existing[c.Name]; ok {
			j.entries[host][idx] = entry
		} else {
			j.entries[host] = append(j.entries[host], entry)
		}
	}
}

func (j *persistentJar) Cookies(u *url.URL) []*http.Cookie {
	return j.inner.Cookies(u)
}

func (j *persistentJar) save() error {
	data, err := json.MarshalIndent(j.entries, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(j.file, data, 0o600)
}

func usage() {
	fmt.Fprintf(os.Stderr, `Usage: dot-http [flags] <file.http> [request-name]

Flags:
  -v               Print version and exit
  -V               Verbose: print request details before sending
  -H               Print response headers only
  -B               Print response body only
  -o <file>        Save response body to file
  --env <name>     Load <name>.env instead of .env
  --timeout <dur>  Request timeout (e.g. 30s, 1m). Default: no timeout
  --no-follow      Do not follow redirects
  --session <file> Load/save cookies from/to a JSON session file
  --junit <file>   Write JUnit XML test report to file
`)
}

func main() {
	_ = version.Set()

	if len(os.Args) < 2 {
		usage()
		os.Exit(1)
	}

	cfg := &Config{}
	args := os.Args[1:]

	for len(args) > 0 && strings.HasPrefix(args[0], "-") {
		switch args[0] {
		case "-v":
			fmt.Println(version.Version)
			os.Exit(0)
		case "-V":
			cfg.verbose = true
		case "-H":
			cfg.mode = outputHeaders
		case "-B":
			cfg.mode = outputBody
		case "-o":
			if len(args) < 2 {
				fmt.Fprintln(os.Stderr, "-o requires a filename")
				os.Exit(1)
			}
			cfg.outputFile = args[1]
			args = args[1:]
		case "--env":
			if len(args) < 2 {
				fmt.Fprintln(os.Stderr, "--env requires a name")
				os.Exit(1)
			}
			cfg.envName = args[1]
			args = args[1:]
		case "--timeout":
			if len(args) < 2 {
				fmt.Fprintln(os.Stderr, "--timeout requires a duration")
				os.Exit(1)
			}
			d, err := time.ParseDuration(args[1])
			if err != nil {
				fmt.Fprintf(os.Stderr, "Invalid timeout %q: %v\n", args[1], err)
				os.Exit(1)
			}
			cfg.timeout = d
			args = args[1:]
		case "--no-follow":
			cfg.noFollow = true
		case "--session":
			if len(args) < 2 {
				fmt.Fprintln(os.Stderr, "--session requires a filename")
				os.Exit(1)
			}
			cfg.sessionFile = args[1]
			args = args[1:]
		case "--junit":
			if len(args) < 2 {
				fmt.Fprintln(os.Stderr, "--junit requires a filename")
				os.Exit(1)
			}
			cfg.junitFile = args[1]
			args = args[1:]
		default:
			fmt.Fprintf(os.Stderr, "Unknown flag: %s\n", args[0])
			os.Exit(1)
		}
		args = args[1:]
	}

	if len(args) < 1 {
		usage()
		os.Exit(1)
	}

	// Load environment file
	envFile := ".env"
	if cfg.envName != "" {
		envFile = cfg.envName + ".env"
	}
	_ = godotenv.Load(envFile)

	filePath := args[0]
	var targetName string
	if len(args) > 1 {
		targetName = args[1]
	}

	data, err := os.ReadFile(filePath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error reading file: %v\n", err)
		os.Exit(1)
	}
	text := string(data)

	baseDir, _ := filepath.Abs(filepath.Dir(filePath))
	absFilePath, _ := filepath.Abs(filePath)
	visited := map[string]bool{absFilePath: true}
	importedBlocks, importedVars, err := parseImports(text, baseDir, visited)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error processing imports: %v\n", err)
		os.Exit(1)
	}

	localBlocks := parseDocumentRequests(text)
	blocks := append(importedBlocks, localBlocks...)

	if len(blocks) == 0 {
		fmt.Fprintln(os.Stderr, "No request blocks found")
		os.Exit(1)
	}

	var target *RequestBlock
	if targetName != "" {
		for i := range blocks {
			if blocks[i].Name == targetName {
				target = &blocks[i]
				break
			}
		}
		if target == nil {
			fmt.Fprintf(os.Stderr, "Request '%s' not found\n", targetName)
			os.Exit(1)
		}
	} else {
		target = &blocks[len(blocks)-1]
	}

	fileVars := parseFileVariables(text)
	envVars := loadEnvFile(filepath.Dir(filePath), cfg.envName)
	sysVars := loadSystemEnv()

	context := make(map[string]any)
	for k, v := range importedVars {
		context[k] = v
	}
	for k, v := range fileVars {
		context[k] = v
	}
	for k, v := range sysVars {
		context[k] = v
	}
	for k, v := range envVars {
		context[k] = v
	}

	// Build HTTP client
	client, jar := buildClient(cfg)
	defer func() {
		if jar != nil {
			_ = jar.save()
		}
	}()

	var testResults []TestResult
	performer := makePerformer(client, cfg)

	result, err := executeRequestChain(target, blocks, context, performer, make(map[string]bool), &testResults)

	// Write JUnit report regardless of error
	if cfg.junitFile != "" {
		if werr := writeJUnit(cfg.junitFile, filePath, testResults); werr != nil {
			fmt.Fprintf(os.Stderr, "Warning: could not write JUnit report: %v\n", werr)
		}
	}

	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	if result != nil {
		if cfg.outputFile != "" {
			if err := os.WriteFile(cfg.outputFile, []byte(result.Body), 0o644); err != nil {
				fmt.Fprintf(os.Stderr, "Error writing output file: %v\n", err)
				os.Exit(1)
			}
		} else {
			printResponse(result, cfg.mode)
		}
	}
}

func buildClient(cfg *Config) (*http.Client, *persistentJar) {
	client := &http.Client{
		Timeout: cfg.timeout,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: false},
		},
	}

	if cfg.noFollow {
		client.CheckRedirect = func(_ *http.Request, _ []*http.Request) error {
			return http.ErrUseLastResponse
		}
	}

	var jar *persistentJar
	if cfg.sessionFile != "" {
		j, err := newPersistentJar(cfg.sessionFile)
		if err == nil {
			jar = j
			client.Jar = jar
		}
	}

	return client, jar
}

func makePerformer(client *http.Client, cfg *Config) func(string) (*ResponseData, error) {
	return func(text string) (*ResponseData, error) {
		return performRequest(text, client, cfg)
	}
}

func executeRequestChain(
	target *RequestBlock,
	allBlocks []RequestBlock,
	context map[string]any,
	performer func(string) (*ResponseData, error),
	visited map[string]bool,
	testResults *[]TestResult,
) (*ResponseData, error) {
	if target.Name != "" {
		if visited[target.Name] {
			return nil, fmt.Errorf("circular dependency detected: %s", target.Name)
		}
		visited[target.Name] = true
	}

	for _, dep := range target.Requires {
		if visited[dep.Name] {
			return nil, fmt.Errorf("circular dependency detected: %s", dep.Name)
		}

		depBlock := findBlock(allBlocks, dep.Name)
		if depBlock == nil {
			return nil, fmt.Errorf("dependency not found: %s", dep.Name)
		}

		depArgs := make(map[string]string)
		for k, v := range dep.Args {
			depArgs[k] = substituteVariables(v, context)
		}

		childContext := copyContext(context)
		for k, v := range depArgs {
			childContext[k] = v
		}

		forceExecute := len(depArgs) > 0

		if forceExecute || context[dep.Name] == nil {
			childVisited := copyVisited(visited)
			_, err := executeRequestChain(depBlock, allBlocks, childContext, performer, childVisited, testResults)
			if err != nil {
				return nil, err
			}

			for _, b := range allBlocks {
				if b.Name != "" {
					if val, ok := childContext[b.Name]; ok {
						if _, isMap := val.(map[string]any); isMap {
							context[b.Name] = val
						}
					}
				}
			}
		}
	}

	resolvedText := substituteVariables(target.Text, context)

	result, err := performer(resolvedText)
	if err != nil {
		return nil, err
	}

	// Check assertions
	if target.ExpectStatus != 0 || len(target.Assertions) > 0 {
		name := target.Name
		if name == "" {
			name = "(unnamed)"
		}
		failures := checkAssertions(target, result)
		*testResults = append(*testResults, TestResult{Name: name, Failures: failures})
		if len(failures) > 0 {
			return result, fmt.Errorf("assertion failed in %q: %s", name, strings.Join(failures, "; "))
		}
	}

	// Store result in context if named
	if target.Name != "" && result != nil {
		var jsonBody any
		if err := json.Unmarshal([]byte(result.Body), &jsonBody); err != nil {
			jsonBody = result.Body
		}

		headers := make(map[string]any)
		for k, v := range result.Headers {
			if len(v) == 1 {
				headers[k] = v[0]
			} else {
				headers[k] = strings.Join(v, ", ")
			}
		}

		responseObj := map[string]any{
			"body":    jsonBody,
			"headers": headers,
			"response": map[string]any{
				"body":    jsonBody,
				"headers": headers,
			},
		}
		context[target.Name] = responseObj
	}

	return result, nil
}

func checkAssertions(block *RequestBlock, result *ResponseData) []string {
	var failures []string

	if block.ExpectStatus != 0 && result.StatusCode != block.ExpectStatus {
		failures = append(failures, fmt.Sprintf("expected status %d, got %d", block.ExpectStatus, result.StatusCode))
	}

	for _, a := range block.Assertions {
		got := extractAssertValue(a.Target, result)
		if !evalOp(got, a.Op, a.Value) {
			failures = append(failures, fmt.Sprintf("%s %s %q (got: %q)", a.Target, a.Op, a.Value, got))
		}
	}

	return failures
}

func extractAssertValue(target string, result *ResponseData) string {
	if target == "status" {
		return strconv.Itoa(result.StatusCode)
	}
	if strings.HasPrefix(target, "header.") {
		return result.Headers.Get(strings.TrimPrefix(target, "header."))
	}
	if strings.HasPrefix(target, "body.") {
		path := strings.TrimPrefix(target, "body.")
		var jsonBody map[string]any
		if json.Unmarshal([]byte(result.Body), &jsonBody) != nil {
			return ""
		}
		parts := strings.Split(path, ".")
		var current any = jsonBody
		for _, p := range parts {
			m, ok := current.(map[string]any)
			if !ok {
				return ""
			}
			current, ok = m[p]
			if !ok {
				return ""
			}
		}
		return fmt.Sprintf("%v", current)
	}
	return ""
}

func evalOp(got, op, expected string) bool {
	switch op {
	case "==":
		return got == expected
	case "!=":
		return got != expected
	case "contains":
		return strings.Contains(got, expected)
	case "!contains":
		return !strings.Contains(got, expected)
	}
	return false
}

func writeJUnit(file, suiteName string, results []TestResult) error {
	suite := junitTestSuite{Name: suiteName}
	for _, r := range results {
		tc := junitTestCase{Name: r.Name}
		suite.Tests++
		if len(r.Failures) > 0 {
			suite.Failures++
			tc.Failure = &junitFailure{
				Message: r.Failures[0],
				Text:    strings.Join(r.Failures, "\n"),
			}
		}
		suite.Cases = append(suite.Cases, tc)
	}

	suites := junitTestSuites{Suites: []junitTestSuite{suite}}
	data, err := xml.MarshalIndent(suites, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(file, append([]byte(xml.Header), data...), 0o644)
}

func findBlock(blocks []RequestBlock, name string) *RequestBlock {
	for i := range blocks {
		if blocks[i].Name == name {
			return &blocks[i]
		}
	}
	return nil
}

func copyContext(src map[string]any) map[string]any {
	dst := make(map[string]any, len(src))
	for k, v := range src {
		dst[k] = v
	}
	return dst
}

func copyVisited(src map[string]bool) map[string]bool {
	dst := make(map[string]bool, len(src))
	for k, v := range src {
		dst[k] = v
	}
	return dst
}

var (
	nameRegex     = regexp.MustCompile(`^\s*#\s*@name\s*=\s*(\w+)`)
	requiresRegex = regexp.MustCompile(`^\s*#\s*@requires\s*=\s*(\w+)(?:\((.*)\))?`)
	variableRegex = regexp.MustCompile(`^\s*@([^\s=]+)\s*=\s*(.+?)\s*$`)
	importRegex   = regexp.MustCompile(`^\s*@import\s*=\s*(.+?)\s*$`)
	substituteRe  = regexp.MustCompile(`\{\{(?:\$env\s+)?([^\s}]+)\}\}`)
	expectRegex   = regexp.MustCompile(`^\s*#\s*@expect\s+(\d+)`)
	assertRegex   = regexp.MustCompile(`^\s*#\s*@assert\s+(\S+)\s+(==|!=|contains|!contains)\s+(.+?)\s*$`)
)

func parseDocumentRequests(text string) []RequestBlock {
	lines := strings.Split(text, "\n")
	var blocks []RequestBlock
	var currentLines []string

	processBlock := func() {
		if len(currentLines) == 0 {
			return
		}
		blockText := strings.Join(currentLines, "\n")

		var name string
		var requires []Dependency
		var assertions []Assertion
		expectStatus := 0

		for _, l := range currentLines {
			if m := nameRegex.FindStringSubmatch(l); m != nil {
				name = m[1]
			}
			if m := requiresRegex.FindStringSubmatch(l); m != nil {
				dep := Dependency{Name: m[1], Args: make(map[string]string)}
				if m[2] != "" {
					for _, part := range strings.Split(m[2], ",") {
						kv := strings.SplitN(strings.TrimSpace(part), "=", 2)
						if len(kv) == 2 {
							dep.Args[strings.TrimSpace(kv[0])] = strings.TrimSpace(kv[1])
						}
					}
				}
				requires = append(requires, dep)
			}
			if m := expectRegex.FindStringSubmatch(l); m != nil {
				expectStatus, _ = strconv.Atoi(m[1])
			}
			if m := assertRegex.FindStringSubmatch(l); m != nil {
				assertions = append(assertions, Assertion{Target: m[1], Op: m[2], Value: m[3]})
			}
		}

		blocks = append(blocks, RequestBlock{
			Name:         name,
			Requires:     requires,
			Text:         blockText,
			ExpectStatus: expectStatus,
			Assertions:   assertions,
		})
	}

	for _, line := range lines {
		line = strings.TrimRight(line, "\r")
		if strings.HasPrefix(strings.TrimSpace(line), "###") {
			processBlock()
			currentLines = nil
		} else {
			currentLines = append(currentLines, line)
		}
	}
	processBlock()

	return blocks
}

func parseImports(text string, baseDir string, visited map[string]bool) ([]RequestBlock, map[string]string, error) {
	var allBlocks []RequestBlock
	allVars := make(map[string]string)

	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimRight(line, "\r")
		if m := importRegex.FindStringSubmatch(line); m != nil {
			importPath := m[1]
			if !filepath.IsAbs(importPath) {
				importPath = filepath.Join(baseDir, importPath)
			}

			absPath, err := filepath.Abs(importPath)
			if err != nil {
				return nil, nil, fmt.Errorf("resolving import path %s: %w", importPath, err)
			}

			if visited[absPath] {
				continue
			}
			visited[absPath] = true

			data, err := os.ReadFile(absPath)
			if err != nil {
				return nil, nil, fmt.Errorf("reading import %s: %w", importPath, err)
			}
			importText := string(data)

			importDir := filepath.Dir(absPath)
			nestedBlocks, nestedVars, err := parseImports(importText, importDir, visited)
			if err != nil {
				return nil, nil, err
			}
			for k, v := range nestedVars {
				allVars[k] = v
			}
			allBlocks = append(allBlocks, nestedBlocks...)

			for k, v := range parseFileVariables(importText) {
				allVars[k] = v
			}
			allBlocks = append(allBlocks, parseDocumentRequests(importText)...)
		}
	}

	return allBlocks, allVars, nil
}

func parseFileVariables(text string) map[string]string {
	vars := make(map[string]string)
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimRight(line, "\r")
		if m := variableRegex.FindStringSubmatch(line); m != nil {
			vars[m[1]] = m[2]
		}
	}
	return vars
}

func substituteVariables(text string, variables map[string]any) string {
	return substituteRe.ReplaceAllStringFunc(text, func(match string) string {
		inner := substituteRe.FindStringSubmatch(match)
		if inner == nil {
			return match
		}
		varName := inner[1]

		if strings.Contains(varName, ".") {
			parts := strings.Split(varName, ".")
			var current any = variables
			for _, part := range parts {
				switch m := current.(type) {
				case map[string]any:
					val, ok := m[part]
					if !ok {
						return match
					}
					current = val
				default:
					return match
				}
			}
			return fmt.Sprintf("%v", current)
		}

		if val, ok := variables[varName]; ok {
			return fmt.Sprintf("%v", val)
		}
		return match
	})
}

func loadEnvFile(dir, envName string) map[string]string {
	vars := make(map[string]string)
	filename := ".env"
	if envName != "" {
		filename = envName + ".env"
	}
	envPath := filepath.Join(dir, filename)

	f, err := os.Open(envPath)
	if err != nil {
		return vars
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		eqIdx := strings.Index(line, "=")
		if eqIdx <= 0 {
			continue
		}
		key := strings.TrimSpace(line[:eqIdx])
		value := strings.TrimSpace(line[eqIdx+1:])
		if len(value) >= 2 {
			if (value[0] == '"' && value[len(value)-1] == '"') ||
				(value[0] == '\'' && value[len(value)-1] == '\'') {
				value = value[1 : len(value)-1]
			}
		}
		vars[key] = value
	}
	return vars
}

func loadSystemEnv() map[string]string {
	vars := make(map[string]string)
	for _, entry := range os.Environ() {
		parts := strings.SplitN(entry, "=", 2)
		if len(parts) == 2 {
			vars[parts[0]] = parts[1]
		}
	}
	return vars
}

func parseRequest(text string) (method, rawURL string, headers map[string]string, body string, ok bool) {
	lines := strings.Split(text, "\n")
	headers = make(map[string]string)

	methodLineIdx := -1
	for i, line := range lines {
		trimmed := strings.TrimSpace(strings.TrimRight(line, "\r"))
		if trimmed != "" && !strings.HasPrefix(trimmed, "#") && !strings.HasPrefix(trimmed, "//") {
			methodLineIdx = i
			break
		}
	}
	if methodLineIdx == -1 {
		return
	}

	methodLine := strings.TrimSpace(lines[methodLineIdx])
	parts := strings.Fields(methodLine)
	if len(parts) < 2 {
		return
	}

	method = parts[0]
	rawURL = parts[1]

	bodyStartIdx := -1
	for i := methodLineIdx + 1; i < len(lines); i++ {
		trimmed := strings.TrimSpace(strings.TrimRight(lines[i], "\r"))
		if trimmed == "" {
			bodyStartIdx = i + 1
			break
		}
		colonIdx := strings.Index(trimmed, ":")
		if colonIdx > 0 {
			key := strings.TrimSpace(trimmed[:colonIdx])
			value := strings.TrimSpace(trimmed[colonIdx+1:])
			headers[key] = value
		}
	}

	if bodyStartIdx > 0 && bodyStartIdx < len(lines) {
		body = strings.Join(lines[bodyStartIdx:], "\n")
	}

	ok = true
	return
}

func performRequest(requestText string, client *http.Client, cfg *Config) (*ResponseData, error) {
	method, rawURL, headers, body, ok := parseRequest(requestText)
	if !ok {
		return nil, fmt.Errorf("invalid request format")
	}

	// GraphQL: wrap plain query body as JSON
	if ct, exists := headers["Content-Type"]; exists && strings.Contains(ct, "application/graphql") {
		queryJSON, err := json.Marshal(map[string]string{"query": strings.TrimSpace(body)})
		if err == nil {
			body = string(queryJSON)
			headers["Content-Type"] = "application/json"
		}
	}

	if cfg.verbose {
		fmt.Fprintf(os.Stderr, "> %s %s\n", method, rawURL)
		for k, v := range headers {
			fmt.Fprintf(os.Stderr, "> %s: %s\n", k, v)
		}
		if body != "" {
			fmt.Fprintf(os.Stderr, ">\n%s\n", strings.TrimSpace(body))
		}
		fmt.Fprintln(os.Stderr)
	}

	var bodyReader io.Reader
	if body != "" {
		bodyReader = strings.NewReader(body)
	}

	req, err := http.NewRequest(method, rawURL, bodyReader)
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}

	for k, v := range headers {
		req.Header.Set(k, v)
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("executing request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading response: %w", err)
	}

	return &ResponseData{
		StatusCode:    resp.StatusCode,
		StatusMessage: resp.Status,
		Headers:       resp.Header,
		Body:          string(respBody),
	}, nil
}

func printResponse(resp *ResponseData, mode outputMode) {
	if mode != outputBody {
		fmt.Printf("%s\n", resp.StatusMessage)
		for k, v := range resp.Headers {
			fmt.Printf("%s: %s\n", k, strings.Join(v, ", "))
		}
	}

	if mode == outputHeaders {
		return
	}

	if mode != outputBody {
		fmt.Println()
	}

	var jsonData any
	if err := json.Unmarshal([]byte(resp.Body), &jsonData); err == nil {
		pretty, err := json.MarshalIndent(jsonData, "", "  ")
		if err == nil {
			fmt.Println(string(pretty))
			return
		}
	}
	fmt.Println(resp.Body)
}
