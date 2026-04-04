
import * as assert from 'assert';
import { parseFileVariables, substituteVariables } from '../extension';

suite('Variable Parser Test Suite', () => {
    test('parseFileVariables should parse variables', () => {
        const text = `
@URL = https://api.example.com
@token = 12345

###
GET {{URL}}/users
Authorization: Bearer {{token}}
`;
        const variables = parseFileVariables(text);
        assert.strictEqual(variables['URL'], 'https://api.example.com');
        assert.strictEqual(variables['token'], '12345');
    });

    test('parseFileVariables should ignore comments', () => {
        const text = `
# @URL = https://api.example.com
// @token = 12345
@valid = true
`;
        const variables = parseFileVariables(text);
        assert.strictEqual(variables['URL'], undefined);
        assert.strictEqual(variables['token'], undefined);
        assert.strictEqual(variables['valid'], 'true');
    });

    test('substituteVariables should replace variables', () => {
        const variables = {
            'URL': 'https://api.example.com',
            'token': '12345'
        };
        const text = 'GET {{URL}}/users?token={{token}}';
        const result = substituteVariables(text, variables);
        assert.strictEqual(result, 'GET https://api.example.com/users?token=12345');
    });

    test('substituteVariables should support {{$env VAR}} syntax', () => {
        const variables = {
            'USERNAME': 'zlatko'
        };
        const text = 'User: {{$env USERNAME}}';
        const result = substituteVariables(text, variables);
        assert.strictEqual(result, 'User: zlatko');
    });

    test('substituteVariables should support dot notation for object traversal', () => {
        const variables = {
            'login': {
                'body': {
                    'token': 'abc-123',
                    'user': {
                        'id': 10
                    }
                },
                'headers': {
                    'Content-Type': 'application/json'
                }
            }
        };
        
        // Test body access
        const text1 = 'Bearer {{login.body.token}}';
        assert.strictEqual(substituteVariables(text1, variables), 'Bearer abc-123');
        
        // Test nested body access
        const text2 = 'User ID: {{login.body.user.id}}';
        assert.strictEqual(substituteVariables(text2, variables), 'User ID: 10');
        
        // Test headers access
        const text3 = 'Type: {{login.headers.Content-Type}}';
        assert.strictEqual(substituteVariables(text3, variables), 'Type: application/json');
    });

    test('substituteVariables should return original text if path invalid', () => {
        const variables = {
            'login': {
                'body': {}
            }
        };
        
        const text = 'Token: {{login.body.token}}';
        assert.strictEqual(substituteVariables(text, variables), 'Token: {{login.body.token}}');
    });

    test('substituteVariables should use variables map which already has precedence applied', () => {
        const fileVars = { 'HOST': 'localhost' };
        const systemVars = { 'HOST': 'system-host', 'USER': 'system-user' };
        const envVars = { 'HOST': 'env-host' };
        
        const combined = { ...fileVars, ...systemVars, ...envVars };
        
        const text = 'Host: {{HOST}}, User: {{USER}}';
        const result = substituteVariables(text, combined);
        
        assert.strictEqual(result, 'Host: env-host, User: system-user');
    });

    test('substituteVariables should keep unknown variables', () => {
        const variables = {
            'URL': 'https://api.example.com'
        };
        const text = 'GET {{URL}}/users?token={{token}}';
        const result = substituteVariables(text, variables);
        assert.strictEqual(result, 'GET https://api.example.com/users?token={{token}}');
    });
});
