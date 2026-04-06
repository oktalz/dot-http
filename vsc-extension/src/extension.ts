
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
    headers: Record<string, string | string[]>;
    body: string;
}

export interface Dependency {
    name: string;
    args: Record<string, string>;
}

export interface Assertion {
    target: string;
    op: '==' | '!=' | 'contains' | '!contains';
    value: string;
}

export interface RequestBlock {
    name?: string;
    requires: Dependency[];
    text: string;
    startLine: number;
    endLine: number;
    expectStatus?: number;
    assertions?: Assertion[];
    noFollow?: boolean;
}

interface FileConfig {
    envName?: string;
    sessionFile?: string;
}

interface CookieEntry {
    name: string;
    value: string;
    domain: string;
    path: string;
    secure: boolean;
    httpOnly: boolean;
    expires?: number;
}

class CookieJar {
    private cookies: Map<string, CookieEntry[]> = new Map();
    private file?: string;

    constructor(file?: string) {
        this.file = file;
        if (file && fs.existsSync(file)) {
            try {
                const data = JSON.parse(fs.readFileSync(file, 'utf8'));
                for (const [domain, entries] of Object.entries(data)) {
                    this.cookies.set(domain, entries as CookieEntry[]);
                }
            } catch { /* ignore */ }
        }
    }

    getCookieHeader(url: URL): string {
        const domain = url.hostname;
        const urlPath = url.pathname;
        const isSecure = url.protocol === 'https:';
        const entries = this.cookies.get(domain) || [];
        return entries
            .filter(c => {
                if (c.secure && !isSecure) { return false; }
                if (!urlPath.startsWith(c.path || '/')) { return false; }
                if (c.expires && Date.now() > c.expires) { return false; }
                return true;
            })
            .map(c => `${c.name}=${c.value}`)
            .join('; ');
    }

    setCookies(url: URL, setCookieHeaders: string[]) {
        const domain = url.hostname;
        const existing = this.cookies.get(domain) || [];
        const existingMap = new Map(existing.map(c => [c.name, c]));

        for (const header of setCookieHeaders) {
            const parts = header.split(';').map(s => s.trim());
            const [nameVal, ...attrs] = parts;
            const eqIdx = nameVal.indexOf('=');
            if (eqIdx < 0) { continue; }
            const name = nameVal.substring(0, eqIdx).trim();
            const value = nameVal.substring(eqIdx + 1).trim();

            const entry: CookieEntry = { name, value, domain, path: '/', secure: false, httpOnly: false };
            for (const attr of attrs) {
                const attrLower = attr.toLowerCase();
                if (attrLower === 'secure') { entry.secure = true; }
                else if (attrLower === 'httponly') { entry.httpOnly = true; }
                else if (attrLower.startsWith('path=')) { entry.path = attr.substring(5); }
                else if (attrLower.startsWith('expires=')) {
                    const d = new Date(attr.substring(8));
                    if (!isNaN(d.getTime())) { entry.expires = d.getTime(); }
                }
            }
            existingMap.set(name, entry);
        }
        this.cookies.set(domain, Array.from(existingMap.values()));
    }

    save() {
        if (!this.file) { return; }
        const data: Record<string, CookieEntry[]> = {};
        for (const [domain, entries] of this.cookies) {
            data[domain] = entries;
        }
        try {
            fs.writeFileSync(this.file, JSON.stringify(data, null, 2));
        } catch { /* ignore */ }
    }
}

export function activate(context: vscode.ExtensionContext) {
    console.log('dot-http active');

    context.subscriptions.push(
        vscode.languages.registerCodeLensProvider({ language: 'http', scheme: 'file' }, new HttpCodeLensProvider())
    );

    context.subscriptions.push(
        vscode.workspace.registerTextDocumentContentProvider(RESPONSE_SCHEME, responseProvider)
    );

    context.subscriptions.push(vscode.commands.registerCommand('dot-http.runRequest', async (codeLensRange: vscode.Range) => {
        const editor = vscode.window.activeTextEditor;
        if (!editor) { return; }

        const document = editor.document;
        const text = document.getText();
        const documentDir = path.dirname(document.uri.fsPath);

        // Parse file-level config (@env, @session)
        const fileConfig = parseFileConfig(text);

        // Process imports
        const visited = new Set<string>();
        visited.add(path.resolve(document.uri.fsPath));
        const imported = parseImports(text, documentDir, visited);

        const localBlocks = parseDocumentRequests(text);
        const requestBlocks = [...imported.blocks, ...localBlocks];

        const targetBlock = localBlocks.find(b =>
            codeLensRange.start.line >= b.startLine && codeLensRange.start.line <= b.endLine
        );

        if (!targetBlock) {
            vscode.window.showErrorMessage("Could not find request block");
            return;
        }

        // Resolve env file
        const envFileName = fileConfig.envName ? `${fileConfig.envName}.env` : '.env';
        const workspaceRoot = vscode.workspace.workspaceFolders?.[0]?.uri.fsPath || documentDir;

        const fileVariables = parseFileVariables(text);
        const envVariables = loadEnvFile(workspaceRoot, envFileName);
        const systemVariables = process.env as Record<string, string>;

        const globalVariables = {
            ...imported.variables,
            ...fileVariables,
            ...systemVariables,
            ...envVariables
        };

        // Cookie jar
        const cookieJar = fileConfig.sessionFile
            ? new CookieJar(path.resolve(workspaceRoot, fileConfig.sessionFile))
            : undefined;

        // Build performer
        const config = vscode.workspace.getConfiguration('dot-http');
        const timeoutMs = parseTimeout(config.get<string>('timeout') || '');
        const followRedirects = config.get<boolean>('followRedirects') ?? true;

        const requestContext: Record<string, any> = { ...globalVariables };
        const assertionResults: { blockName: string; failures: string[] }[] = [];

        const performer = (reqText: string, block: RequestBlock) =>
            performRequest(reqText, { timeoutMs, followRedirects: block.noFollow ? false : followRedirects, cookieJar });

        try {
            const result = await executeRequestChain(targetBlock, requestBlocks, requestContext, performer, new Set(), assertionResults);
            cookieJar?.save();
            if (result) {
                await showResult(result, assertionResults);
            }
        } catch (error: any) {
            cookieJar?.save();
            vscode.window.showErrorMessage(`Error: ${error.message}`);
        }
    }));
}

export async function executeRequestChain(
    targetBlock: RequestBlock,
    allBlocks: RequestBlock[],
    context: Record<string, any>,
    performer: (text: string, block: RequestBlock) => Promise<ResponseData | null>,
    visited: Set<string> = new Set(),
    assertionResults: { blockName: string; failures: string[] }[] = []
): Promise<ResponseData | null> {
    if (targetBlock.name && visited.has(targetBlock.name)) {
        throw new Error(`Circular dependency detected: ${targetBlock.name}`);
    }
    if (targetBlock.name) { visited.add(targetBlock.name); }

    for (const dep of targetBlock.requires) {
        if (visited.has(dep.name)) {
            throw new Error(`Circular dependency detected: ${dep.name}`);
        }
        const dependencyBlock = allBlocks.find(b => b.name === dep.name);
        if (!dependencyBlock) {
            throw new Error(`Dependency not found: ${dep.name}`);
        }

        const depArgs: Record<string, string> = {};
        for (const [key, val] of Object.entries(dep.args)) {
            depArgs[key] = substituteVariables(val, context);
        }

        const childContext = { ...context, ...depArgs };
        const forceExecute = Object.keys(depArgs).length > 0;

        if (forceExecute || !context[dep.name]) {
            await executeRequestChain(dependencyBlock, allBlocks, childContext, performer, new Set(visited), assertionResults);
            for (const key in childContext) {
                const val = childContext[key];
                if (val && typeof val === 'object' && allBlocks.some(b => b.name === key)) {
                    context[key] = val;
                }
            }
        }
    }

    const resolvedRequestText = substituteVariables(targetBlock.text, context);
    const result = await performer(resolvedRequestText, targetBlock);
    if (!result) { return null; }

    // Check assertions
    if (targetBlock.expectStatus !== undefined || (targetBlock.assertions?.length ?? 0) > 0) {
        const name = targetBlock.name || '(unnamed)';
        const failures = checkAssertions(targetBlock, result);
        assertionResults.push({ blockName: name, failures });
        if (failures.length > 0) {
            throw new Error(`Assertion failed in "${name}": ${failures.join('; ')}`);
        }
    }

    if (targetBlock.name) {
        let jsonBody: any = null;
        try { jsonBody = JSON.parse(result.body); } catch { /* ignore */ }
        context[targetBlock.name] = {
            body: jsonBody ?? result.body,
            headers: result.headers,
            response: { body: jsonBody ?? result.body, headers: result.headers }
        };
    }

    return result;
}

function checkAssertions(block: RequestBlock, result: ResponseData): string[] {
    const failures: string[] = [];

    if (block.expectStatus !== undefined && result.statusCode !== block.expectStatus) {
        failures.push(`expected status ${block.expectStatus}, got ${result.statusCode}`);
    }

    for (const a of block.assertions ?? []) {
        const got = extractAssertValue(a.target, result);
        if (!evalOp(got, a.op, a.value)) {
            failures.push(`${a.target} ${a.op} "${a.value}" (got: "${got}")`);
        }
    }
    return failures;
}

function extractAssertValue(target: string, result: ResponseData): string {
    if (target === 'status') { return String(result.statusCode); }
    if (target.startsWith('header.')) {
        const name = target.substring(7);
        const val = result.headers[name] ?? result.headers[name.toLowerCase()];
        return Array.isArray(val) ? val.join(', ') : (val || '');
    }
    if (target.startsWith('body.')) {
        const jsonPath = target.substring(5);
        try {
            let obj = JSON.parse(result.body);
            for (const part of jsonPath.split('.')) {
                if (obj && typeof obj === 'object') { obj = obj[part]; }
                else { return ''; }
            }
            return String(obj ?? '');
        } catch { return ''; }
    }
    return '';
}

function evalOp(got: string, op: string, expected: string): boolean {
    switch (op) {
        case '==': return got === expected;
        case '!=': return got !== expected;
        case 'contains': return got.includes(expected);
        case '!contains': return !got.includes(expected);
    }
    return false;
}

class HttpCodeLensProvider implements vscode.CodeLensProvider {
    provideCodeLenses(document: vscode.TextDocument): vscode.CodeLens[] {
        const lenses: vscode.CodeLens[] = [];
        const text = document.getText();
        const requestBlocks = parseDocumentRequests(text);
        const methodRegex = /^(GET|POST|PUT|DELETE|PATCH|HEAD|OPTIONS|CONNECT|TRACE)\s+/;

        for (const block of requestBlocks) {
            const lines = block.text.split(/\r?\n/);
            for (let i = 0; i < lines.length; i++) {
                if (methodRegex.test(lines[i])) {
                    const absLine = block.startLine + i;
                    const range = new vscode.Range(absLine, 0, absLine, lines[i].length);
                    lenses.push(new vscode.CodeLens(range, {
                        title: "Run Request",
                        command: "dot-http.runRequest",
                        arguments: [range]
                    }));
                    break;
                }
            }
        }
        return lenses;
    }
}

function parseFileConfig(text: string): FileConfig {
    const config: FileConfig = {};
    const envRegex = /^\s*@env\s*=\s*(.+?)\s*$/m;
    const sessionRegex = /^\s*@session\s*=\s*(.+?)\s*$/m;
    const envMatch = text.match(envRegex);
    if (envMatch) { config.envName = envMatch[1]; }
    const sessionMatch = text.match(sessionRegex);
    if (sessionMatch) { config.sessionFile = sessionMatch[1]; }
    return config;
}

function parseDocumentRequests(text: string): RequestBlock[] {
    const lines = text.split(/\r?\n/);
    const blocks: RequestBlock[] = [];
    let currentLines: string[] = [];
    let currentStartLine = 0;

    const nameRegex = /^\s*#\s*@name\s*=\s*(\w+)/;
    const requiresRegex = /^\s*#\s*@requires\s*=\s*(\w+)(?:\((.*)\))?/;
    const expectRegex = /^\s*#\s*@expect\s+(\d+)/;
    const assertRegex = /^\s*#\s*@assert\s+(\S+)\s+(==|!=|contains|!contains)\s+(.+?)\s*$/;
    const noFollowRegex = /^\s*#\s*@no-follow/;

    const processBlock = () => {
        if (currentLines.length === 0) { return; }
        const blockText = currentLines.join('\n');
        let name: string | undefined;
        const requires: Dependency[] = [];
        const assertions: Assertion[] = [];
        let expectStatus: number | undefined;
        let noFollow = false;

        for (const l of currentLines) {
            const nameMatch = l.match(nameRegex);
            if (nameMatch) { name = nameMatch[1]; }

            const requiresMatch = l.match(requiresRegex);
            if (requiresMatch) {
                const args: Record<string, string> = {};
                if (requiresMatch[2]) {
                    for (const part of requiresMatch[2].split(',')) {
                        const [k, v] = part.split('=').map(s => s.trim());
                        if (k && v) { args[k] = v; }
                    }
                }
                requires.push({ name: requiresMatch[1], args });
            }

            const expectMatch = l.match(expectRegex);
            if (expectMatch) { expectStatus = parseInt(expectMatch[1], 10); }

            const assertMatch = l.match(assertRegex);
            if (assertMatch) {
                assertions.push({ target: assertMatch[1], op: assertMatch[2] as Assertion['op'], value: assertMatch[3] });
            }

            if (noFollowRegex.test(l)) { noFollow = true; }
        }

        blocks.push({
            text: blockText,
            startLine: currentStartLine,
            endLine: currentStartLine + currentLines.length - 1,
            name,
            requires,
            expectStatus,
            assertions,
            noFollow
        });
    };

    for (let i = 0; i <= lines.length; i++) {
        const line = i < lines.length ? lines[i] : '###';
        if (line.trim().startsWith('###')) {
            processBlock();
            currentLines = [];
            currentStartLine = i + 1;
        } else {
            currentLines.push(line);
        }
    }
    return blocks;
}

async function performRequest(
    requestText: string,
    opts: { timeoutMs: number; followRedirects: boolean; cookieJar?: CookieJar }
): Promise<ResponseData | null> {
    const parsed = parseRequest(requestText);
    if (!parsed) { throw new Error("Invalid request format"); }

    let { method, url, headers, body } = parsed;

    // GraphQL: auto-wrap
    const ct = headers['Content-Type'] || headers['content-type'] || '';
    if (ct.includes('application/graphql') && body) {
        body = JSON.stringify({ query: body.trim() });
        headers['Content-Type'] = 'application/json';
        delete headers['content-type'];
    }

    return makeRequest(method, url, headers, body, opts);
}

async function showResult(
    responseData: ResponseData,
    assertionResults: { blockName: string; failures: string[] }[]
) {
    const headerLines = Object.entries(responseData.headers)
        .map(([k, v]) => `${k}: ${Array.isArray(v) ? v.join(', ') : v}`);

    let body = responseData.body;
    try {
        body = JSON.stringify(JSON.parse(body), null, 2);
    } catch { /* not JSON */ }

    const parts = [
        `HTTP/1.1 ${responseData.statusCode} ${responseData.statusMessage}`,
        ...headerLines,
        '',
        body
    ];

    if (assertionResults.length > 0) {
        parts.push('', '--- Assertions ---');
        for (const r of assertionResults) {
            if (r.failures.length === 0) {
                parts.push(`✓ ${r.blockName}: all passed`);
            } else {
                for (const f of r.failures) {
                    parts.push(`✗ ${r.blockName}: ${f}`);
                }
            }
        }
    }

    const content = parts.join('\n');
    const config = vscode.workspace.getConfiguration('dot-http');
    const viewMode = config.get<string>('responseViewMode') || 'reuseTab';

    if (viewMode === 'output') {
        if (!outputChannel) { outputChannel = vscode.window.createOutputChannel("dot-http"); }
        outputChannel.clear();
        outputChannel.append(content);
        outputChannel.show(true);
    } else if (viewMode === 'reuseTab') {
        const uri = vscode.Uri.parse(`${RESPONSE_SCHEME}:response.http`);
        responseProvider.update(uri, content);
        const doc = await vscode.workspace.openTextDocument(uri);
        await vscode.window.showTextDocument(doc, { viewColumn: vscode.ViewColumn.Beside, preview: true, preserveFocus: true });
    } else {
        const doc = await vscode.workspace.openTextDocument({ content, language: 'http' });
        await vscode.window.showTextDocument(doc, { viewColumn: vscode.ViewColumn.Beside });
    }
}

function parseRequest(text: string): { method: string; url: string; headers: Record<string, string>; body: string | null } | null {
    const lines = text.split(/\r?\n/);
    let methodLineIndex = -1;

    for (let i = 0; i < lines.length; i++) {
        const line = lines[i].trim();
        if (line && !line.startsWith('#') && !line.startsWith('//')) {
            methodLineIndex = i;
            break;
        }
    }
    if (methodLineIndex === -1) { return null; }

    const methodParts = lines[methodLineIndex].trim().split(/\s+/);
    if (methodParts.length < 2) { return null; }

    const method = methodParts[0];
    const url = methodParts[1];
    const headers: Record<string, string> = {};
    let bodyStartIndex = -1;

    for (let i = methodLineIndex + 1; i < lines.length; i++) {
        const line = lines[i].trim();
        if (line === '') { bodyStartIndex = i + 1; break; }
        const colonIndex = line.indexOf(':');
        if (colonIndex > 0) {
            headers[line.substring(0, colonIndex).trim()] = line.substring(colonIndex + 1).trim();
        }
    }

    const body = bodyStartIndex !== -1 && bodyStartIndex < lines.length
        ? lines.slice(bodyStartIndex).join('\n')
        : null;

    return { method, url, headers, body };
}

function makeRequest(
    method: string,
    requestUrl: string,
    headers: Record<string, string>,
    body: string | null,
    opts: { timeoutMs: number; followRedirects: boolean; cookieJar?: CookieJar },
    redirectCount = 0
): Promise<ResponseData> {
    return new Promise((resolve, reject) => {
        let parsedUrl: URL;
        try { parsedUrl = new URL(requestUrl); } catch (e) { return reject(new Error(`Invalid URL: ${requestUrl}`)); }

        // Inject cookies
        if (opts.cookieJar) {
            const cookieHeader = opts.cookieJar.getCookieHeader(parsedUrl);
            if (cookieHeader) { headers['Cookie'] = cookieHeader; }
        }

        const options: http.RequestOptions = {
            method,
            headers,
            hostname: parsedUrl.hostname,
            port: parsedUrl.port || (parsedUrl.protocol === 'https:' ? 443 : 80),
            path: parsedUrl.pathname + parsedUrl.search,
            protocol: parsedUrl.protocol,
            ...(opts.timeoutMs > 0 ? { timeout: opts.timeoutMs } : {})
        };

        const lib = parsedUrl.protocol === 'https:' ? https : http;

        const req = lib.request(options, (res) => {
            // Handle Set-Cookie
            if (opts.cookieJar && res.headers['set-cookie']) {
                const setCookies = Array.isArray(res.headers['set-cookie'])
                    ? res.headers['set-cookie']
                    : [res.headers['set-cookie']];
                opts.cookieJar.setCookies(parsedUrl, setCookies);
            }

            // Handle redirects
            const isRedirect = [301, 302, 303, 307, 308].includes(res.statusCode || 0);
            if (opts.followRedirects && isRedirect && res.headers.location && redirectCount < 10) {
                const location = res.headers.location;
                const nextUrl = location.startsWith('http') ? location : new URL(location, requestUrl).toString();
                const nextMethod = res.statusCode === 303 ? 'GET' : method;
                const nextBody = res.statusCode === 303 ? null : body;
                res.resume(); // drain
                resolve(makeRequest(nextMethod, nextUrl, headers, nextBody, opts, redirectCount + 1));
                return;
            }

            const chunks: Buffer[] = [];
            res.on('data', (chunk) => chunks.push(chunk));
            res.on('end', () => {
                const responseBody = Buffer.concat(chunks).toString();
                const responseHeaders: Record<string, string | string[]> = {};
                for (const [k, v] of Object.entries(res.headers)) {
                    if (v !== undefined) { responseHeaders[k] = v; }
                }
                resolve({
                    statusCode: res.statusCode || 0,
                    statusMessage: res.statusMessage || '',
                    headers: responseHeaders,
                    body: responseBody
                });
            });
        });

        req.on('timeout', () => { req.destroy(new Error(`Request timed out after ${opts.timeoutMs}ms`)); });
        req.on('error', (e) => reject(e));
        if (body) { req.write(body); }
        req.end();
    });
}

export function parseImports(text: string, baseDir: string, visited: Set<string> = new Set()): { blocks: RequestBlock[]; variables: Record<string, string> } {
    const allBlocks: RequestBlock[] = [];
    const allVars: Record<string, string> = {};
    const importRegex = /^\s*@import\s*=\s*(.+?)\s*$/;

    for (const line of text.split(/\r?\n/)) {
        const match = line.match(importRegex);
        if (!match) { continue; }

        let importPath = match[1];
        if (!path.isAbsolute(importPath)) { importPath = path.join(baseDir, importPath); }
        const absPath = path.resolve(importPath);
        if (visited.has(absPath) || !fs.existsSync(absPath)) { continue; }
        visited.add(absPath);

        const importText = fs.readFileSync(absPath, 'utf8');
        const importDir = path.dirname(absPath);
        const nested = parseImports(importText, importDir, visited);
        Object.assign(allVars, nested.variables);
        allBlocks.push(...nested.blocks);
        Object.assign(allVars, parseFileVariables(importText));
        allBlocks.push(...parseDocumentRequests(importText));
    }

    return { blocks: allBlocks, variables: allVars };
}

export function parseFileVariables(text: string): Record<string, string> {
    const variables: Record<string, string> = {};
    const reserved = new Set(['import', 'env', 'session']);
    const variableRegex = /^\s*@([^\s=]+)\s*=\s*(.+?)\s*$/;

    for (const line of text.split(/\r?\n/)) {
        const match = line.match(variableRegex);
        if (match && !reserved.has(match[1])) {
            variables[match[1]] = match[2];
        }
    }
    return variables;
}

export function substituteVariables(text: string, variables: Record<string, any>): string {
    return text.replace(/\{\{(?:\$env\s+)?([^\s}]+)\}\}/g, (match, variableName) => {
        if (variableName.includes('.')) {
            const parts = variableName.split('.');
            let current = variables;
            for (const part of parts) {
                if (current && typeof current === 'object' && part in current) {
                    current = current[part];
                } else { return match; }
            }
            return String(current);
        }
        if (Object.prototype.hasOwnProperty.call(variables, variableName)) {
            return String(variables[variableName]);
        }
        return match;
    });
}

export function loadEnvFile(rootPath: string, fileName = '.env'): Record<string, string> {
    const envPath = path.join(rootPath, fileName);
    const variables: Record<string, string> = {};

    if (!fs.existsSync(envPath)) { return variables; }
    const content = fs.readFileSync(envPath, 'utf8');

    for (const line of content.split(/\r?\n/)) {
        const trimmed = line.trim();
        if (!trimmed || trimmed.startsWith('#')) { continue; }
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
    return variables;
}

function parseTimeout(s: string): number {
    if (!s) { return 0; }
    const match = s.match(/^(\d+)(ms|s|m)?$/);
    if (!match) { return 0; }
    const n = parseInt(match[1], 10);
    switch (match[2]) {
        case 'ms': return n;
        case 'm': return n * 60000;
        default: return n * 1000; // seconds
    }
}

export function deactivate() {}
