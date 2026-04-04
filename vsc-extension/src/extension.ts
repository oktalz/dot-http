
import * as vscode from 'vscode';
import * as http from 'http';
import * as https from 'https';
import * as fs from 'fs';
import * as path from 'path';
import { URL } from 'url';

const RESPONSE_SCHEME = 'http-client-response';

class ResponseContentProvider implements vscode.TextDocumentContentProvider {
    private _onDidChange = new vscode.EventEmitter<vscode.Uri>();
    readonly onDidChange = this._onDidChange.event;
    private contentMap = new Map<string, string>();

    provideTextDocumentContent(uri: vscode.Uri): string {
        return this.contentMap.get(uri.toString()) || '';
    }

    update(uri: vscode.Uri, content: string) {
        this.contentMap.set(uri.toString(), content);
        this._onDidChange.fire(uri);
    }
}

const responseProvider = new ResponseContentProvider();
let outputChannel: vscode.OutputChannel;

export interface ResponseData {
    statusCode: number;
    statusMessage: string;
    headers: any;
    body: string;
}

export interface Dependency {
    name: string;
    args: Record<string, string>;
}

export interface RequestBlock {
    name?: string;
    requires: Dependency[];
    text: string;
    startLine: number;
    endLine: number;
}

export function activate(context: vscode.ExtensionContext) {
    console.log('http-client active');

    context.subscriptions.push(
        vscode.languages.registerCodeLensProvider({ language: 'http', scheme: 'file' }, new HttpCodeLensProvider())
    );
    
    context.subscriptions.push(
        vscode.workspace.registerTextDocumentContentProvider(RESPONSE_SCHEME, responseProvider)
    );

    context.subscriptions.push(vscode.commands.registerCommand('dot-http.runRequest', async (codeLensRange: vscode.Range) => {
        const editor = vscode.window.activeTextEditor;
        if (!editor) {
            return;
        }
        
        const document = editor.document;
        const text = document.getText();
        const documentDir = path.dirname(document.uri.fsPath);

        // Process imports
        const visited = new Set<string>();
        visited.add(path.resolve(document.uri.fsPath));
        const imported = parseImports(text, documentDir, visited);

        // Parse all request blocks (imported blocks first, then local)
        const localBlocks = parseDocumentRequests(text);
        const requestBlocks = [...imported.blocks, ...localBlocks];

        // Find the block corresponding to the CodeLens (only in local blocks)
        const targetBlock = localBlocks.find(b =>
            codeLensRange.start.line >= b.startLine && codeLensRange.start.line <= b.endLine
        );

        if (!targetBlock) {
            vscode.window.showErrorMessage("Could not find request block");
            return;
        }

        // Prepare global variables: imported vars < file vars < system env < .env
        const fileVariables = parseFileVariables(text);
        let envVariables: Record<string, string> = {};
        if (vscode.workspace.workspaceFolders && vscode.workspace.workspaceFolders.length > 0) {
            const workspaceRoot = vscode.workspace.workspaceFolders[0].uri.fsPath;
            envVariables = loadEnvFile(workspaceRoot);
        }
        const systemVariables = process.env as Record<string, string>;

        const globalVariables = {
            ...imported.variables,
            ...fileVariables,
            ...systemVariables,
            ...envVariables
        };
        
        // Execute Chain
        const requestContext: Record<string, any> = { ...globalVariables };
        
        try {
            const result = await executeRequestChain(targetBlock, requestBlocks, requestContext, performRequest);
            if (result) {
                await showResult(result);
            }
        } catch (error: any) {
            vscode.window.showErrorMessage(`Error: ${error.message}`);
        }
    }));
}

export async function executeRequestChain(
    targetBlock: RequestBlock, 
    allBlocks: RequestBlock[], 
    context: Record<string, any>,
    requestPerformer: (text: string) => Promise<ResponseData | null>,
    visited: Set<string> = new Set()
) {
    // Check for circular dependencies
    if (targetBlock.name && visited.has(targetBlock.name)) {
        throw new Error(`Circular dependency detected: ${targetBlock.name}`);
    }
    
    if (targetBlock.name) {
        visited.add(targetBlock.name);
    }
    
    // Execute dependencies first
    for (const dep of targetBlock.requires) {
        const dependencyName = dep.name;
        
        // Circular check for dependency (basic check)
        if (visited.has(dependencyName)) {
             throw new Error(`Circular dependency detected: ${dependencyName}`);
        }

        const dependencyBlock = allBlocks.find(b => b.name === dependencyName);
        if (!dependencyBlock) {
            throw new Error(`Dependency not found: ${dependencyName}`);
        }
        
        // Prepare context for dependency execution
        // Substitute arguments using current context
        const depArgs: Record<string, string> = {};
        for (const [key, val] of Object.entries(dep.args)) {
            depArgs[key] = substituteVariables(val, context);
        }
        
        // Create child context with overridden variables
        const childContext = { ...context, ...depArgs };
        
        // Execute dependency
        // Note: We always execute if args are present (parameterized dependency), 
        // OR if not executed yet.
        // If args are present, we treat it as a new execution? 
        // User said: "# @requires=first(id={{varname}}) and # @requires=first(id={{varname2}}) should both be called"
        // This implies we force execution.
        // But if no args, maybe we cache?
        // To support "both be called", we must execute.
        // However, `executeRequestChain` stores result in `context[name]`.
        // If called twice, last one overwrites. This is acceptable for immediate variable usage in target block?
        // But side effects happen twice.
        
        const forceExecute = Object.keys(depArgs).length > 0;
        
        if (forceExecute || !context[dependencyName]) {
             await executeRequestChain(dependencyBlock, allBlocks, childContext, requestPerformer, new Set(visited));
             
             // Merge results back to parent context
             // Only copy ResponseData objects (results)
             // We verify it's an object and matches a known request name
             for (const key in childContext) {
                 const val = childContext[key];
                 if (val && typeof val === 'object') {
                     const isRequestName = allBlocks.some(b => b.name === key);
                     if (isRequestName) {
                         context[key] = val;
                     }
                 }
             }
        }
    }
    
    // Substitute variables
    const resolvedRequestText = substituteVariables(targetBlock.text, context);
    
    // Execute Request
    const result = await requestPerformer(resolvedRequestText);
    
    if (!result) {
        // Parse error or something
        return;
    }
    
    // Store result in context if named
    if (targetBlock.name) {
        let jsonBody = null;
        try {
            jsonBody = JSON.parse(result.body);
        } catch (e) {
            // ignore
        }
        
        context[targetBlock.name] = {
            body: jsonBody || result.body,
            headers: result.headers,
            response: { // Add response wrapper just in case standard format requires it
                body: jsonBody || result.body,
                headers: result.headers
            }
        };
    }
    
    // If this is the originally requested block (how do we know? We are in recursive function)
    // We should separate "execute dependency" from "execute target".
    // But since we want to recurse, maybe we can just display the result if it's the *last* one executed in the stack?
    // Actually, `executeRequestChain` creates a promise chain. The top-level call waits for everything.
    // We can return the result from this function.
    
    return result;
}

// Redefine runRequest logic to handle display
// We need to move the display logic out or handle it at top level.

class HttpCodeLensProvider implements vscode.CodeLensProvider {
    provideCodeLenses(document: vscode.TextDocument, token: vscode.CancellationToken): vscode.CodeLens[] {
        const lenses: vscode.CodeLens[] = [];
        const text = document.getText();
        const requestBlocks = parseDocumentRequests(text);
        
        for (const block of requestBlocks) {
            // We want the lens to appear at the start of the method line or block
            // `parseDocumentRequests` should return exact method line location or we search for it.
            // Let's iterate lines in the block to find the method line.
            
            const methodRegex = /^(GET|POST|PUT|DELETE|PATCH|HEAD|OPTIONS|CONNECT|TRACE)\s+/;
            const lines = block.text.split(/\r?\n/);
            
            for(let i=0; i<lines.length; i++) {
                if (methodRegex.test(lines[i])) {
                    // Calculate absolute line number
                    const absLine = block.startLine + i;
                    const range = new vscode.Range(absLine, 0, absLine, lines[i].length);
                    const cmd: vscode.Command = {
                        title: "Run Request",
                        command: "dot-http.runRequest",
                        arguments: [range]
                    };
                    lenses.push(new vscode.CodeLens(range, cmd));
                    break;
                }
            }
        }
        return lenses;
    }
}

function parseDocumentRequests(text: string): RequestBlock[] {
    const lines = text.split(/\r?\n/);
    const blocks: RequestBlock[] = [];
    let currentLines: string[] = [];
    let currentStartLine = 0;
    
    for (let i = 0; i <= lines.length; i++) {
        const line = i < lines.length ? lines[i] : '###'; // Force process last block
        
        if (line.trim().startsWith('###')) {
            if (currentLines.length > 0) {
                // Process previous block
                const blockText = currentLines.join('\n');
                
                // Parse metadata
                let name: string | undefined;
                const requires: Dependency[] = [];
                
                const nameRegex = /^\s*#\s*@name\s*=\s*(\w+)/;
                // Regex handles: # @requires=name(arg1=val1, arg2=val2)
                const requiresRegex = /^\s*#\s*@requires\s*=\s*(\w+)(?:\((.*)\))?/;
                
                for (const l of currentLines) {
                    const nameMatch = l.match(nameRegex);
                    if (nameMatch) {
                        name = nameMatch[1];
                    }
                    
                    const requiresMatch = l.match(requiresRegex);
                    if (requiresMatch) {
                        const reqName = requiresMatch[1];
                        const argsStr = requiresMatch[2];
                        const args: Record<string, string> = {};
                        
                        if (argsStr) {
                            // Simple parsing of comma separated key=value
                            // Note: Does not handle commas inside quotes for simplicity
                            const parts = argsStr.split(',');
                            for (const part of parts) {
                                const [k, v] = part.split('=').map(s => s.trim());
                                if (k && v) {
                                    args[k] = v;
                                }
                            }
                        }
                        requires.push({ name: reqName, args });
                    }
                }
                
                blocks.push({
                    text: blockText,
                    startLine: currentStartLine,
                    endLine: currentStartLine + currentLines.length - 1,
                    name,
                    requires
                });
            }
            currentLines = [];
            currentStartLine = i + 1;
        } else {
            currentLines.push(line);
        }
    }
    return blocks;
}

// Renamed from executeRequest
async function performRequest(requestText: string): Promise<ResponseData | null> {
    const parsed = parseRequest(requestText);
    if (!parsed) {
        // Only show error if it's the target request? Or for dependencies too?
        // For dependencies, if they fail parsing, the chain fails.
        throw new Error("Invalid request format");
    }

    const { method, url, headers, body } = parsed;
    
    // We can't await executeRequest because it returns void in current impl.
    // We need makeRequest.
    const responseData = await makeRequest(method, url, headers, body);
    return responseData;
}

// Legacy executeRequest for backward compatibility or direct usage? 
// No, we are replacing the command implementation.
// But we need a function to SHOW the result.

async function showResult(responseData: ResponseData) {
    const content = [
        `HTTP/1.1 ${responseData.statusCode} ${responseData.statusMessage}`,
        ...Object.entries(responseData.headers).map(([k, v]) => `${k}: ${v}`),
        '',
        responseData.body
    ].join('\n');
    
    const config = vscode.workspace.getConfiguration('http-client');
    const viewMode = config.get<string>('responseViewMode') || 'reuseTab';
    
    if (viewMode === 'output') {
        if (!outputChannel) {
            outputChannel = vscode.window.createOutputChannel("Http Client");
        }
        outputChannel.clear();
        outputChannel.append(content);
        outputChannel.show(true);
    } else if (viewMode === 'reuseTab') {
        const uri = vscode.Uri.parse(`${RESPONSE_SCHEME}:response.http`);
        responseProvider.update(uri, content);
        const doc = await vscode.workspace.openTextDocument(uri);
        await vscode.window.showTextDocument(doc, { viewColumn: vscode.ViewColumn.Beside, preview: true, preserveFocus: true });
    } else {
        // newTab
        const doc = await vscode.workspace.openTextDocument({ content: content, language: 'http' });
        await vscode.window.showTextDocument(doc, { viewColumn: vscode.ViewColumn.Beside });
    }
}

// ... parseRequest and makeRequest definitions remain (almost) same ...

function parseRequest(text: string) {
    const lines = text.split(/\r?\n/);
    let methodLineIndex = -1;
    
    // Find first non-empty, non-comment line
    for (let i = 0; i < lines.length; i++) {
        const line = lines[i].trim();
        if (line && !line.startsWith('#') && !line.startsWith('//')) {
            methodLineIndex = i;
            break;
        }
    }
    
    if (methodLineIndex === -1) {
        return null;
    }
    
    const methodLine = lines[methodLineIndex].trim();
    const methodParts = methodLine.split(/\s+/);
    if (methodParts.length < 2) {
        return null;
    }
    
    const method = methodParts[0];
    const url = methodParts[1]; 
    
    const headers: Record<string, string> = {};
    let bodyStartIndex = -1;
    
    for (let i = methodLineIndex + 1; i < lines.length; i++) {
        const line = lines[i].trim();
        if (line === '') {
            bodyStartIndex = i + 1;
            break;
        }
        
        const colonIndex = line.indexOf(':');
        if (colonIndex > 0) {
            const key = line.substring(0, colonIndex).trim();
            const value = line.substring(colonIndex + 1).trim();
            headers[key] = value;
        }
    }
    
    let body = null;
    if (bodyStartIndex !== -1 && bodyStartIndex < lines.length) {
        body = lines.slice(bodyStartIndex).join('\n');
    }
    
    return { method, url, headers, body };
}

function makeRequest(method: string, requestUrl: string, headers: any, body: string | null): Promise<ResponseData> {
    return new Promise((resolve, reject) => {
        const parsedUrl = new URL(requestUrl);
        const options: http.RequestOptions | https.RequestOptions = {
            method: method,
            headers: headers,
            hostname: parsedUrl.hostname,
            port: parsedUrl.port || (parsedUrl.protocol === 'https:' ? 443 : 80),
            path: parsedUrl.pathname + parsedUrl.search,
            protocol: parsedUrl.protocol
        };

        const lib = parsedUrl.protocol === 'https:' ? https : http;
        
        const req = lib.request(options, (res) => {
            const chunks: any[] = [];
            res.on('data', (chunk) => chunks.push(chunk));
            res.on('end', () => {
                const bodyBuffer = Buffer.concat(chunks);
                // Try to detect encoding or default to utf-8
                const responseBody = bodyBuffer.toString(); 
                resolve({
                    statusCode: res.statusCode || 0,
                    statusMessage: res.statusMessage || '',
                    headers: res.headers,
                    body: responseBody
                });
            });
        });
        
        req.on('error', (e) => reject(e));
        
        if (body) {
            req.write(body);
        }
        
        req.end();
    });
}

export function parseImports(text: string, baseDir: string, visited: Set<string> = new Set()): { blocks: RequestBlock[]; variables: Record<string, string> } {
    const allBlocks: RequestBlock[] = [];
    const allVars: Record<string, string> = {};
    const importRegex = /^\s*@import\s*=\s*(.+?)\s*$/;

    for (const line of text.split(/\r?\n/)) {
        const match = line.match(importRegex);
        if (match) {
            let importPath = match[1];
            if (!path.isAbsolute(importPath)) {
                importPath = path.join(baseDir, importPath);
            }

            const absPath = path.resolve(importPath);
            if (visited.has(absPath)) {
                continue; // skip already-imported files to avoid cycles
            }
            visited.add(absPath);

            if (!fs.existsSync(absPath)) {
                continue;
            }
            const importText = fs.readFileSync(absPath, 'utf8');

            // Recursively resolve imports in the imported file
            const importDir = path.dirname(absPath);
            const nested = parseImports(importText, importDir, visited);
            Object.assign(allVars, nested.variables);
            allBlocks.push(...nested.blocks);

            // Parse the imported file's own blocks and variables
            Object.assign(allVars, parseFileVariables(importText));
            allBlocks.push(...parseDocumentRequests(importText));
        }
    }

    return { blocks: allBlocks, variables: allVars };
}

export function parseFileVariables(text: string): Record<string, string> {
    const variables: Record<string, string> = {};
    const lines = text.split(/\r?\n/);
    const variableRegex = /^\s*@([^\s=]+)\s*=\s*(.+?)\s*$/;
    
    for (const line of lines) {
        const match = line.match(variableRegex);
        if (match) {
            variables[match[1]] = match[2];
        }
    }
    return variables;
}

export function substituteVariables(text: string, variables: Record<string, any>): string {
    return text.replace(/\{\{(?:\$env\s+)?([^\s}]+)\}\}/g, (match, variableName) => {
        // Dot notation support
        if (variableName.includes('.')) {
            const parts = variableName.split('.');
            let current = variables;
            for (const part of parts) {
                if (current && typeof current === 'object' && part in current) {
                    current = current[part];
                } else {
                    return match;
                }
            }
            return String(current);
        }
        
        if (variables.hasOwnProperty(variableName)) {
            return String(variables[variableName]);
        }
        return match;
    });
}

export function loadEnvFile(rootPath: string): Record<string, string> {
    const envPath = path.join(rootPath, '.env');
    const variables: Record<string, string> = {};
    
    if (fs.existsSync(envPath)) {
        const content = fs.readFileSync(envPath, 'utf8');
        const lines = content.split(/\r?\n/);
        for (const line of lines) {
            const trimmed = line.trim();
            if (!trimmed || trimmed.startsWith('#')) {
                continue;
            }
            
            const eqIndex = trimmed.indexOf('=');
            if (eqIndex > 0) {
                const key = trimmed.substring(0, eqIndex).trim();
                let value = trimmed.substring(eqIndex + 1).trim();
                
                if ((value.startsWith('"') && value.endsWith('"')) || (value.startsWith("'") && value.endsWith("'"))) {
                    value = value.substring(1, value.length - 1);
                }
                
                variables[key] = value;
            }
        }
    }
    return variables;
}

export function deactivate() {}
