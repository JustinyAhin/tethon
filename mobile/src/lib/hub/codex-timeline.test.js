import assert from 'node:assert/strict';
import { test } from 'node:test';

import { buildCodexTimeline } from './codex-timeline';

const itemEvent = (seq, turnId, id, body) => ({
  seq,
  type: 'codex_event',
  payload: {
    method: 'item/completed',
    turn_id: turnId,
    item_id: id,
    params: { item: { id, type: 'agentMessage', text: body } },
  },
});

test('keeps paginated native history authoritative while a new turn streams', () => {
  const turns = [
    {
      id: 'loaded-turn',
      status: 'completed',
      items: [
        {
          id: 'loaded-message',
          type: 'agentMessage',
          text: 'Native history',
        },
      ],
    },
  ];
  const events = [
    itemEvent(
      1,
      'older-unloaded-turn',
      'old-message',
      'Old delivery fragment',
    ),
    itemEvent(
      2,
      'loaded-turn',
      'loaded-message',
      'Duplicate delivery text',
    ),
    itemEvent(3, 'active-turn', 'active-message', 'Live message'),
  ];

  const entries = buildCodexTimeline({
    events,
    turns,
    currentTurnId: 'active-turn',
  });

  assert.deepEqual(
    entries.map((entry) => entry.text),
    ['Native history', 'Live message'],
  );

  // Native history can come from the engine without a matching delivery event.
  const reconnected = buildCodexTimeline({
    turns,
    events: [events[0], events[2]],
    currentTurnId: 'active-turn',
  });

  assert.deepEqual(
    reconnected.map((entry) => entry.text),
    ['Native history', 'Live message'],
  );
});

test('keeps the first-message delivery warning visible through history replay', () => {
  const message =
    'input delivery is unconfirmed; reconcile native history before any new attempt';
  const events = [
    {
      seq: 8,
      type: 'codex_event',
      payload: { method: 'tethon/inputError', params: { message } },
    },
  ];

  for (const turns of [
    [],
    [{ id: 'recovered', status: 'completed', items: [] }],
  ]) {
    const entries = buildCodexTimeline({ events, turns });
    const warning = entries.find(
      (entry) => entry.title === 'Message needs review',
    );

    assert.ok(warning?.warning);
    assert.equal(warning.text, message);
    assert.match(warning.detail, /Check native history/);
    assert.doesNotMatch(warning.text, /cannot display/);
  }
});
