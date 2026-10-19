import assert from 'node:assert/strict';
import { test } from 'node:test';

import type { Event } from '@tethon/hub-sdk/protocol';
import { mergeTimeline } from './codex';

test('native replay merges snapshots and final items without duplicate text or invented turns', () => {
	const event = (seq: number, method: string, params: unknown): Event => ({
		session_id: 'session',
		seq,
		type: 'codex_event',
		created_at: '2026-10-19T00:00:00Z',
		payload: { method, params }
	});

	const turns = mergeTimeline({
		history: [
			{
				id: 'old',
				status: 'inProgress',
				items: [{ id: 'message', type: 'agentMessage', text: 'Hello' }]
			},
			{ id: 'new', status: 'completed', items: [] }
		],
		events: [
			event(1, 'item/started', {
				turnId: 'old',
				item: { id: 'message', type: 'agentMessage', text: '' }
			}),
			event(2, 'item/agentMessage/delta', { turnId: 'old', itemId: 'message', delta: 'Hello' }),
			event(3, 'item/agentMessage/delta', { turnId: 'old', itemId: 'message', delta: ' world' }),
			event(4, 'item/completed', {
				turnId: 'old',
				item: { id: 'message', type: 'agentMessage', text: 'Hello world.' }
			}),
			event(5, 'item/agentMessage/delta', { turnId: 'old', itemId: 'message', delta: 'duplicate' }),
			event(6, 'future/unknown/delta', {
				turnId: 'unknown',
				itemId: 'unknown',
				delta: 'unsupported'
			})
		]
	});

	assert.deepEqual(
		turns.map((turn) => turn.id),
		['old', 'new']
	);
	assert.equal(turns[0].items.length, 1);
	assert.equal(turns[0].items[0].text, 'Hello world.');
});
