
import * as assert from 'assert';
import { completionContext } from '../extension';

suite('Completion Context Test Suite', () => {
    test('inside {{ }} returns variable mode with prefix', () => {
        const ctx = completionContext('GET {{base');
        assert.strictEqual(ctx.kind, 'variable');
        if (ctx.kind === 'variable') { assert.strictEqual(ctx.prefix, 'base'); }
    });

    test('empty {{ }} returns variable mode with empty prefix', () => {
        const ctx = completionContext('Authorization: Bearer {{');
        assert.strictEqual(ctx.kind, 'variable');
        if (ctx.kind === 'variable') { assert.strictEqual(ctx.prefix, ''); }
    });

    test('{{$env VAR}} returns variable mode', () => {
        const ctx = completionContext('User: {{$env USER');
        assert.strictEqual(ctx.kind, 'variable');
        if (ctx.kind === 'variable') { assert.strictEqual(ctx.prefix, 'USER'); }
    });

    test('closed {{ }} does not return variable mode', () => {
        const ctx = completionContext('GET {{base}}/users');
        assert.notStrictEqual(ctx.kind, 'variable');
    });

    test('@requires = returns requestName mode', () => {
        assert.strictEqual(completionContext('@requires = ').kind, 'requestName');
        assert.strictEqual(completionContext('@requires = log').kind, 'requestName');
        assert.strictEqual(completionContext('  @requires = log').kind, 'requestName');
    });

    test('@assert with one token returns assertTarget mode', () => {
        assert.strictEqual(completionContext('@assert ').kind, 'assertTarget');
        assert.strictEqual(completionContext('@assert body').kind, 'assertTarget');
    });

    test('@assert with target and partial op returns assertOp mode', () => {
        assert.strictEqual(completionContext('@assert status ').kind, 'assertOp');
        assert.strictEqual(completionContext('@assert body.name ==').kind, 'assertOp');
    });

    test('@oauth2-grant = returns oauth2Grant mode', () => {
        assert.strictEqual(completionContext('@oauth2-grant = ').kind, 'oauth2Grant');
        assert.strictEqual(completionContext('# @oauth2-grant = client'.replace(/^#\s*/, '')).kind, 'oauth2Grant');
    });

    test('Content-Type header returns contentType mode', () => {
        assert.strictEqual(completionContext('Content-Type: ').kind, 'contentType');
        assert.strictEqual(completionContext('content-type: app').kind, 'contentType');
    });

    test('line starting with @ returns directive mode', () => {
        assert.strictEqual(completionContext('@').kind, 'directive');
        assert.strictEqual(completionContext('@oauth2-').kind, 'directive');
        assert.strictEqual(completionContext('  @nam').kind, 'directive');
    });

    test('start of line returns methodOrHeader mode', () => {
        assert.strictEqual(completionContext('').kind, 'methodOrHeader');
        assert.strictEqual(completionContext('GE').kind, 'methodOrHeader');
        assert.strictEqual(completionContext('Auth').kind, 'methodOrHeader');
    });

    test('directive check wins over methodOrHeader for @-lines', () => {
        // '@name' could match the trailing methodOrHeader regex's spirit, but
        // the directive branch is checked first.
        assert.strictEqual(completionContext('@name').kind, 'directive');
    });

    test('arbitrary mid-line text returns none', () => {
        assert.strictEqual(completionContext('GET https://api.example.com/foo').kind, 'none');
        assert.strictEqual(completionContext('  "title": "foo",').kind, 'none');
    });
});
