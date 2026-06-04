
import * as assert from 'assert';
import * as vscode from 'vscode';
import { computeDiagnostics } from '../extension';

// Directives use the bare `@key = value` form; a leading `#` disables them.
async function docFrom(content: string): Promise<vscode.TextDocument> {
    return vscode.workspace.openTextDocument({ content, language: 'http' });
}

suite('Diagnostics Test Suite', () => {
    test('flags @requires pointing at a missing @name', async () => {
        const doc = await docFrom(`@requires = nope\nGET https://example.com\n`);
        const diags = computeDiagnostics(doc);
        const reqDiag = diags.find(d => d.message.includes('unknown request "nope"'));
        assert.ok(reqDiag, 'expected an unknown-request diagnostic');
        assert.strictEqual(reqDiag!.severity, vscode.DiagnosticSeverity.Error);
    });

    test('accepts @requires pointing at an existing @name', async () => {
        const doc = await docFrom(
            `@name = login\nGET https://example.com/login\n\n###\n@requires = login\nGET https://example.com/me\n`
        );
        const diags = computeDiagnostics(doc);
        assert.ok(!diags.some(d => d.message.includes('unknown request')), 'should not flag a valid @requires');
    });

    test('flags duplicate @name declarations', async () => {
        const doc = await docFrom(
            `@name = dup\nGET https://a.com\n\n###\n@name = dup\nGET https://b.com\n`
        );
        const diags = computeDiagnostics(doc);
        assert.ok(diags.some(d => d.message.includes('Duplicate request name "dup"')));
    });

    test('flags undefined variable references', async () => {
        const doc = await docFrom(`GET https://example.com/{{notDefinedAnywhere123}}\n`);
        const diags = computeDiagnostics(doc);
        assert.ok(diags.some(d => d.message.includes('Undefined variable "notDefinedAnywhere123"')));
    });

    test('does not flag variables defined as file vars', async () => {
        const doc = await docFrom(`@base = https://example.com\nGET {{base}}/x\n`);
        const diags = computeDiagnostics(doc);
        assert.ok(!diags.some(d => d.message.includes('Undefined variable')), 'file var should be recognized');
    });

    test('does not flag named-request response references', async () => {
        const doc = await docFrom(
            `@name = login\nGET https://example.com/login\n\n###\nGET https://example.com/{{login.body.id}}\n`
        );
        const diags = computeDiagnostics(doc);
        assert.ok(!diags.some(d => d.message.includes('Undefined variable')), 'named request ref should be recognized');
    });

    test('flags an invalid @assert operator', async () => {
        const doc = await docFrom(`@assert status === 200\nGET https://example.com\n`);
        const diags = computeDiagnostics(doc);
        assert.ok(diags.some(d => d.message.includes('Invalid @assert operator "==="')));
    });

    test('accepts a valid @assert', async () => {
        const doc = await docFrom(`@assert body.name == John\nGET https://example.com\n`);
        const diags = computeDiagnostics(doc);
        assert.ok(!diags.some(d => d.message.includes('Invalid @assert')), 'valid assert should pass');
    });

    test('does not flag a commented (disabled) @assert', async () => {
        const doc = await docFrom(`# @assert status === 200\nGET https://example.com\n`);
        const diags = computeDiagnostics(doc);
        assert.ok(!diags.some(d => d.message.includes('Invalid @assert')), 'commented assert is disabled');
    });
});
