import type { CodexSessionState } from '@/lib/hub/types';

const nativeSessionLabel = (state: CodexSessionState | undefined): string => {
  if (state === undefined) {
    return 'connecting';
  }

  if (state.conversation_state !== 'ready') {
    return state.conversation_state;
  }

  if (state.connection_state !== 'connected') {
    return state.connection_state;
  }

  switch (state.turn_state) {
    case 'in_progress':
      return 'working';
    case 'interrupted':
      return 'stopped';
    case 'failed':
      return 'failed';
    default:
      return 'ready';
  }
};

export { nativeSessionLabel };
