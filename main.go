package main

import (
	"bufio"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/joho/godotenv"
	"github.com/oktalz/dot-http/version"
)

// ResponseData mirrors the VSCode extension's response structure.
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

// RequestBlock represents a single request block in an .http file.
type RequestBlock struct {
	Name     string
	Requires []Dependency
	Text     string
}

type outputMode int

const (
	outputAll     outputMode = iota
	outputHeaders            // -H: headers only
	outputBody               // -B: body only
)

func main() {
	_ = version.Set() // Initialize version info from build data
	_ = godotenv.Overload(".env")

	if len(os.Args) < 2 {
		fmt.Fprintf(os.Stderr, "Usage: %s [-H|-B] <file.http> [request-name]\n", os.Args[0])
		os.Exit(1)
	}

	mode := outputAll
	args := os.Args[1:]

	// Parse flags
	for len(args) > 0 && strings.HasPrefix(args[0], "-") {
		switch args[0] {
		case "-v":
			fmt.Println(version.Version)
			os.Exit(0)
		case "-H":
			mode = outputHeaders
		case "-B":
			mode = outputBody
		default:
			fmt.Fprintf(os.Stderr, "Unknown flag: %s\n", args[0])
			os.Exit(1)
		}
		args = args[1:]
	}

	if len(args) < 1 {
		fmt.Fprintf(os.Stderr, "Usage: %s [-H|-B] <file.http> [request-name]\n", os.Args[0])
		os.Exit(1)
	}

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

	// Process imports first
	baseDir, _ := filepath.Abs(filepath.Dir(filePath))
	absFilePath, _ := filepath.Abs(filePath)
	visited := map[string]bool{absFilePath: true}
	importedBlocks, importedVars, err := parseImports(text, baseDir, visited)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error processing imports: %v\n", err)
		os.Exit(1)
	}

	// Local blocks come after imported ones so local names take precedence in lookup
	localBlocks := parseDocumentRequests(text)
	blocks := append(importedBlocks, localBlocks...)

	if len(blocks) == 0 {
		fmt.Fprintln(os.Stderr, "No request blocks found")
		os.Exit(1)
	}

	// Find target block
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
		// Run last block by default (like clicking the last request)
		target = &blocks[len(blocks)-1]
	}

	// Build variables: imported vars < file vars < system env < .env file
	fileVars := parseFileVariables(text)
	envVars := loadEnvFile(filepath.Dir(filePath))
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

	result, err := executeRequestChain(target, blocks, context, performRequest, make(map[string]bool))
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	if result != nil {
		printResponse(result, mode)
	}
}

func executeRequestChain(
	target *RequestBlock,
	allBlocks []RequestBlock,
	context map[string]any,
	performer func(string) (*ResponseData, error),
	visited map[string]bool,
) (*ResponseData, error) {
	// Circular dependency check
	if target.Name != "" {
		if visited[target.Name] {
			return nil, fmt.Errorf("circular dependency detected: %s", target.Name)
		}
		visited[target.Name] = true
	}

	// Execute dependencies first
	for _, dep := range target.Requires {
		if visited[dep.Name] {
			return nil, fmt.Errorf("circular dependency detected: %s", dep.Name)
		}

		depBlock := findBlock(allBlocks, dep.Name)
		if depBlock == nil {
			return nil, fmt.Errorf("dependency not found: %s", dep.Name)
		}

		// Substitute argument values using current context
		depArgs := make(map[string]string)
		for k, v := range dep.Args {
			depArgs[k] = substituteVariables(v, context)
		}

		// Create child context with overridden variables
		childContext := copyContext(context)
		for k, v := range depArgs {
			childContext[k] = v
		}

		forceExecute := len(depArgs) > 0

		if forceExecute || context[dep.Name] == nil {
			childVisited := copyVisited(visited)
			_, err := executeRequestChain(depBlock, allBlocks, childContext, performer, childVisited)
			if err != nil {
				return nil, err
			}

			// Merge response results back to parent context
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

	// Substitute variables in request text
	resolvedText := substituteVariables(target.Text, context)

	// Execute request
	result, err := performer(resolvedText)
	if err != nil {
		return nil, err
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

		for _, l := range currentLines {
			if m := nameRegex.FindStringSubmatch(l); m != nil {
				name = m[1]
			}
			if m := requiresRegex.FindStringSubmatch(l); m != nil {
				dep := Dependency{
					Name: m[1],
					Args: make(map[string]string),
				}
				if m[2] != "" {
					parts := strings.Split(m[2], ",")
					for _, part := range parts {
						kv := strings.SplitN(strings.TrimSpace(part), "=", 2)
						if len(kv) == 2 {
							dep.Args[strings.TrimSpace(kv[0])] = strings.TrimSpace(kv[1])
						}
					}
				}
				requires = append(requires, dep)
			}
		}

		blocks = append(blocks, RequestBlock{
			Name:     name,
			Requires: requires,
			Text:     blockText,
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
	processBlock() // last block

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
				continue // skip already-imported files to avoid cycles
			}
			visited[absPath] = true

			data, err := os.ReadFile(absPath)
			if err != nil {
				return nil, nil, fmt.Errorf("reading import %s: %w", importPath, err)
			}
			importText := string(data)

			// Recursively resolve imports in the imported file
			importDir := filepath.Dir(absPath)
			nestedBlocks, nestedVars, err := parseImports(importText, importDir, visited)
			if err != nil {
				return nil, nil, err
			}
			for k, v := range nestedVars {
				allVars[k] = v
			}
			allBlocks = append(allBlocks, nestedBlocks...)

			// Parse the imported file's own blocks and variables
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

		// Dot notation
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

func loadEnvFile(dir string) map[string]string {
	vars := make(map[string]string)
	envPath := filepath.Join(dir, ".env")

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

		// Strip surrounding quotes
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

func parseRequest(text string) (method, url string, headers map[string]string, body string, ok bool) {
	lines := strings.Split(text, "\n")
	headers = make(map[string]string)

	// Find first non-empty, non-comment line
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
	url = parts[1]

	// Parse headers until blank line
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

func performRequest(requestText string) (*ResponseData, error) {
	method, url, headers, body, ok := parseRequest(requestText)
	if !ok {
		return nil, fmt.Errorf("invalid request format")
	}

	var bodyReader io.Reader
	if body != "" {
		bodyReader = strings.NewReader(body)
	}

	req, err := http.NewRequest(method, url, bodyReader)
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}

	for k, v := range headers {
		req.Header.Set(k, v)
	}

	client := &http.Client{
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: false},
		},
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

	// Pretty-print JSON if possible
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
