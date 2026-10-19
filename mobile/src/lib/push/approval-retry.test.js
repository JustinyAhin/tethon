import assert from 'node:assert/strict';
import { test } from 'node:test';

import { createApprovalRetryQueue } from './approval-retry';

const hubUrl = 'https://hub.example.com';
const intent = {
  hostId: 'host-1',
  sessionId: 'session-1',
  requestId: 'request-1',
  action: 'reply',
  text: 'Use the specified file.',
};

const setup = () => {
  let raw = null;
  let time = 1000;
  let storageFails = false;

  const storage = {
    getItem: async () => raw,
    setItem: async (_key, value) => {
      if (storageFails) {
        throw new Error('Storage unavailable');
      }

      raw = value;
    },
  };

  const queue = () => createApprovalRetryQueue({ storage, now: () => time });

  return {
    queue,
    advance: (milliseconds) => {
      time += milliseconds;
    },
    failStorage: (fails) => {
      storageFails = fails;
    },
  };
};

test('retries the exact action after restart and respects persisted backoff', async () => {
  const fixture = setup();
  const first = fixture.queue();
  const sent = [];

  const resolve = async (value) => {
    sent.push(value);
    return 'retry';
  };

  await first.enqueue({ hubUrl, responseKey: 'reply-1', intent });
  await first.flush({ hubUrl, canRun: () => true, resolve });

  assert.equal(sent.length, 1);

  const restarted = fixture.queue();
  await restarted.flush({ hubUrl, canRun: () => true, resolve });

  assert.equal(sent.length, 1);

  fixture.advance(5000);
  await restarted.flush({
    hubUrl,
    canRun: () => true,
    resolve: async (value) => {
      assert.equal(value.hostId, intent.hostId);
      assert.equal(value.sessionId, intent.sessionId);
      assert.equal(value.requestId, intent.requestId);
      assert.equal(value.action, intent.action);
      assert.equal(value.text, intent.text);
      sent.push(value);
      return 'resolved';
    },
  });

  assert.equal(sent.length, 2);
  assert.equal(await restarted.nextAttemptAt(), null);

  const afterSuccess = fixture.queue();
  await afterSuccess.enqueue({ hubUrl, responseKey: 'reply-1', intent });
  await afterSuccess.flush({ hubUrl, canRun: () => true, resolve });

  assert.equal(sent.length, 2);
});

test('serializes concurrent sends and retains the first decision', async () => {
  const fixture = setup();
  const queue = fixture.queue();

  await queue.enqueue({ hubUrl, responseKey: 'reply-1', intent });

  let release = () => {};
  let started = () => {};
  const held = new Promise((resolve) => {
    release = resolve;
  });

  const sending = new Promise((resolve) => {
    started = resolve;
  });

  let sends = 0;
  const resolve = async () => {
    sends += 1;
    started();
    await held;
    return 'resolved';
  };

  const first = queue.flush({ hubUrl, canRun: () => true, resolve });
  await sending;

  const concurrent = queue.flush({ hubUrl, canRun: () => true, resolve });
  await queue.enqueue({
    hubUrl,
    responseKey: 'deny-1',
    intent: { ...intent, action: 'deny' },
  });
  release();
  await Promise.all([first, concurrent]);

  await queue.enqueue({
    hubUrl,
    responseKey: 'approve-1',
    intent: { ...intent, action: 'approve' },
  });
  await queue.flush({ hubUrl, canRun: () => true, resolve });

  assert.equal(sends, 1);
  assert.equal(await queue.nextAttemptAt(), null);
});

test('drops stale actions without sending them after restart', async () => {
  const fixture = setup();
  const first = fixture.queue();
  let sends = 0;

  const resolve = async () => {
    sends += 1;
    return 'stale';
  };

  await first.enqueue({ hubUrl, responseKey: 'reply-1', intent });
  await first.flush({ hubUrl, canRun: () => true, resolve });

  fixture.advance(5000);
  await fixture.queue().flush({ hubUrl, canRun: () => true, resolve });

  assert.equal(sends, 1);
});

test('does not send an old action after restart', async () => {
  const fixture = setup();
  const queue = fixture.queue();
  let sends = 0;

  const resolve = async () => {
    sends += 1;
    return 'resolved';
  };

  await queue.enqueue({ hubUrl, responseKey: 'reply-1', intent });
  fixture.advance(24 * 60 * 60 * 1000);
  await fixture.queue().flush({ hubUrl, canRun: () => true, resolve });

  assert.equal(sends, 0);
});

test('does not send after sign-out or through another hub', async () => {
  const fixture = setup();
  const queue = fixture.queue();
  let sends = 0;

  const resolve = async () => {
    sends += 1;
    return 'resolved';
  };

  await queue.enqueue({ hubUrl, responseKey: 'reply-1', intent });
  await queue.flush({ hubUrl, canRun: () => false, resolve });

  assert.equal(sends, 0);

  await queue.clear();
  await fixture.queue().flush({ hubUrl, canRun: () => true, resolve });
  assert.equal(sends, 0);

  await queue.enqueue({
    hubUrl,
    responseKey: 'reply-2',
    intent: { ...intent, requestId: 'request-2' },
  });
  await queue.flush({
    hubUrl: 'https://other.example.com',
    canRun: () => true,
    resolve,
  });

  assert.equal(sends, 0);
  assert.equal(await queue.nextAttemptAt(), null);
});

test('does not send if storage cannot retain the attempt', async () => {
  const fixture = setup();
  const queue = fixture.queue();
  let sends = 0;

  const resolve = async () => {
    sends += 1;
    return 'resolved';
  };

  fixture.failStorage(true);
  await assert.rejects(
    queue.enqueue({ hubUrl, responseKey: 'reply-1', intent }),
  );

  fixture.failStorage(false);
  await queue.enqueue({ hubUrl, responseKey: 'reply-1', intent });
  fixture.failStorage(true);
  await assert.rejects(queue.flush({ hubUrl, canRun: () => true, resolve }));

  assert.equal(sends, 0);

  fixture.failStorage(false);
  await queue.flush({ hubUrl, canRun: () => true, resolve });

  assert.equal(sends, 1);
});

test('keeps native decisions exact across restart and isolates connection generations', async () => {
  const fixture = setup();
  const queue = fixture.queue();
  const native = {
    kind: 'native',
    hostId: 'host-1',
    sessionId: 'session-1',
    requestId: '9007199254740993',
    connectionId: 'connection-1',
    result: { answers: { target: { answers: ['Preserve this answer.'] } } },
  };

  await queue.enqueue({ hubUrl, responseKey: 'native-1', intent: native });

  const sent = [];
  const resolve = async (value) => {
    sent.push(value);
    return 'stale';
  };

  await fixture.queue().flush({ hubUrl, canRun: () => true, resolve });

  assert.equal(sent[0].requestId, '9007199254740993');
  assert.equal(sent[0].connectionId, 'connection-1');
  assert.deepEqual(sent[0].result, native.result);

  const restarted = fixture.queue();
  await restarted.enqueue({
    hubUrl,
    responseKey: 'native-2',
    intent: { ...native, connectionId: 'connection-2' },
  });
  await restarted.flush({ hubUrl, canRun: () => true, resolve });

  assert.equal(sent.length, 2);
  assert.equal(sent[1].connectionId, 'connection-2');
});
