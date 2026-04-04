
import * as assert from 'assert';
import { executeRequestChain, RequestBlock, ResponseData } from '../extension';

suite('Chain Execution Test Suite', () => {
    
    test('executeRequestChain should execute dependencies recursively', async () => {
        const executionOrder: string[] = [];
        
        // C -> B -> A
        const blockC: RequestBlock = {
            name: 'C',
            requires: [],
            text: 'REQ_C',
            startLine: 0,
            endLine: 1
        };
        
        const blockB: RequestBlock = {
            name: 'B',
            requires: [{ name: 'C', args: {} }],
            text: 'REQ_B',
            startLine: 2,
            endLine: 3
        };
        
        const blockA: RequestBlock = {
            name: 'A',
            requires: [{ name: 'B', args: {} }],
            text: 'REQ_A',
            startLine: 4,
            endLine: 5
        };
        
        const allBlocks = [blockA, blockB, blockC];
        const context: Record<string, any> = {};
        
        const mockPerformer = async (text: string): Promise<ResponseData | null> => {
            if (text.includes('REQ_C')) {
                executionOrder.push('C');
                return {
                    statusCode: 200,
                    statusMessage: 'OK',
                    headers: {},
                    body: '{"val": "fromC"}'
                };
            }
            if (text.includes('REQ_B')) {
                executionOrder.push('B');
                return {
                    statusCode: 200,
                    statusMessage: 'OK',
                    headers: {},
                    body: '{"val": "fromB"}'
                };
            }
            if (text.includes('REQ_A')) {
                executionOrder.push('A');
                return {
                    statusCode: 200,
                    statusMessage: 'OK',
                    headers: {},
                    body: '{"val": "fromA"}'
                };
            }
            return null;
        };
        
        await executeRequestChain(blockA, allBlocks, context, mockPerformer);
        
        assert.deepStrictEqual(executionOrder, ['C', 'B', 'A']);
        
        // Verify context has results
        assert.strictEqual(context['C'].body.val, 'fromC');
        assert.strictEqual(context['B'].body.val, 'fromB');
    });

    test('executeRequestChain should detect circular dependencies', async () => {
        // A -> B -> A
        const blockA: RequestBlock = {
            name: 'A',
            requires: [{ name: 'B', args: {} }],
            text: 'REQ_A',
            startLine: 0,
            endLine: 1
        };
        
        const blockB: RequestBlock = {
            name: 'B',
            requires: [{ name: 'A', args: {} }],
            text: 'REQ_B',
            startLine: 2,
            endLine: 3
        };
        
        const allBlocks = [blockA, blockB];
        const context: Record<string, any> = {};
        const mockPerformer = async () => null;
        
        await assert.rejects(async () => {
            await executeRequestChain(blockA, allBlocks, context, mockPerformer);
        }, /Circular dependency/);
    });

    test('executeRequestChain should reuse executed dependencies', async () => {
        const executionOrder: string[] = [];
        
        // C used by A and B
        // A -> B -> C
        //      |
        //      -> C
        // Logic: A requires B. B requires C.
        // Also A could require C directly? 
        // Let's test: A requires B and C. B requires C.
        
        const blockC: RequestBlock = {
            name: 'C',
            requires: [],
            text: 'REQ_C',
            startLine: 0,
            endLine: 1
        };
        
        const blockB: RequestBlock = {
            name: 'B',
            requires: [{ name: 'C', args: {} }],
            text: 'REQ_B',
            startLine: 2,
            endLine: 3
        };
        
        const blockA: RequestBlock = {
            name: 'A',
            requires: [{ name: 'B', args: {} }],
            text: 'REQ_A',
            startLine: 4,
            endLine: 5
        };
        
        // Let's just test single line dependency structure as implemented.
        // A -> B -> C.
        // Run A. C runs, B runs, A runs.
        // Run A again with same context.
        
        const allBlocks = [blockA, blockB, blockC];
        const context: Record<string, any> = {}; // Start empty
        
        const mockPerformer = async (text: string): Promise<ResponseData | null> => {
            if (text.includes('REQ_C')) {
                executionOrder.push('C');
                return { statusCode: 200, statusMessage: 'OK', headers: {}, body: '{}' };
            }
            if (text.includes('REQ_B')) {
                executionOrder.push('B');
                return { statusCode: 200, statusMessage: 'OK', headers: {}, body: '{}' };
            }
            if (text.includes('REQ_A')) {
                executionOrder.push('A');
                return { statusCode: 200, statusMessage: 'OK', headers: {}, body: '{}' };
            }
            return null;
        };
        
        await executeRequestChain(blockA, allBlocks, context, mockPerformer);
        assert.deepStrictEqual(executionOrder, ['C', 'B', 'A']);
        
        // Reset execution order but keep context
        executionOrder.length = 0;
        
        // Run A again. C and B should be in context, so they shouldn't run. Only A runs.
        await executeRequestChain(blockA, allBlocks, context, mockPerformer);
        assert.deepStrictEqual(executionOrder, ['A']);
    });

    test('executeRequestChain should support multiple parameterized dependencies', async () => {
        const executionOrder: string[] = [];
        
        const blockB: RequestBlock = {
            name: 'B',
            requires: [],
            text: 'REQ_B id={{id}}',
            startLine: 0,
            endLine: 1
        };
        
        const blockA: RequestBlock = {
            name: 'A',
            requires: [
                { name: 'B', args: { 'id': '1' } },
                { name: 'B', args: { 'id': '2' } }
            ],
            text: 'REQ_A',
            startLine: 2,
            endLine: 3
        };
        
        const allBlocks = [blockA, blockB];
        const context: Record<string, any> = {};
        
        const mockPerformer = async (text: string): Promise<ResponseData | null> => {
            if (text.includes('REQ_B')) {
                // Extract id from text "REQ_B id=..."
                const match = text.match(/id=(\d+)/);
                const id = match ? match[1] : 'unknown';
                executionOrder.push(`B(${id})`);
                return { statusCode: 200, statusMessage: 'OK', headers: {}, body: `{"id": "${id}"}` };
            }
            if (text.includes('REQ_A')) {
                executionOrder.push('A');
                return { statusCode: 200, statusMessage: 'OK', headers: {}, body: '{}' };
            }
            return null;
        };
        
        await executeRequestChain(blockA, allBlocks, context, mockPerformer);
        
        assert.deepStrictEqual(executionOrder, ['B(1)', 'B(2)', 'A']);
        
        // Verify context has result of last B call (id=2)
        assert.strictEqual(context['B'].body.id, '2');
    });
});
