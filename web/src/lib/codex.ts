import type { CodexEventPayload, CodexHistoryTurn, Event } from '@tethon/hub-sdk/protocol';

type NativeObject = Record<string, unknown>;
type TimelineTurn = {
	id: string;
	status: string;
	items: NativeObject[];
	plan?: NativeObject[];
	diff?: string;
	error?: unknown;
};

const object = (value: unknown): NativeObject =>
	value !== null && typeof value === 'object' && !Array.isArray(value)
		? (value as NativeObject)
		: {};

const list = (value: unknown): unknown[] => (Array.isArray(value) ? value : []);

const text = (value: unknown): string => (typeof value === 'string' ? value : '');

const label = (value: unknown) =>
	text(value)
		.replace(/([a-z])([A-Z])/g, '$1 $2')
		.replaceAll('_', ' ');

const itemDeltaMethods = new Set([
	'item/agentMessage/delta',
	'item/plan/delta',
	'item/commandExecution/outputDelta',
	'item/reasoning/summaryTextDelta'
]);

const timelineMethods = new Set([
	'turn/started',
	'turn/completed',
	'turn/plan/updated',
	'turn/diff/updated',
	'item/started',
	'item/completed',
	...itemDeltaMethods
]);

const nativeText = (value: unknown): string => {
	if (typeof value === 'string') return value;
	if (Array.isArray(value)) return value.map(nativeText).filter(Boolean).join('\n');
	const data = object(value);
	for (const key of ['text', 'message', 'content', 'output', 'stdout', 'result', 'error']) {
		if (data[key] !== undefined) {
			const result = nativeText(data[key]);
			if (result) return result;
		}
	}
	return '';
};

// Keep history and delivery events independent. Complete native items replace
// deltas by item ID. Prefix comparison avoids repeating snapshot text on replay.

const mergeTimeline = ({
	history,
	events
}: {
	history: CodexHistoryTurn[];
	events: Event[];
}): TimelineTurn[] => {
	const turns = new Map<string, TimelineTurn>();
	for (const turn of history)
		turns.set(turn.id, { ...turn, items: (turn.items ?? []).map(object) });
	const live = new Map<string, TimelineTurn>();
	const completed = new Set<string>();
	for (const event of events) {
		if (event.type !== 'codex_event') continue;
		const payload = event.payload as CodexEventPayload;
		if (payload.truncated || !timelineMethods.has(payload.method)) continue;
		const params = object(payload.params);
		const nativeTurn = object(params.turn);
		const turnId = payload.turn_id || text(params.turnId) || text(nativeTurn.id);
		if (!turnId) continue;
		let turn = live.get(turnId);
		if (!turn) {
			turn = { id: turnId, status: 'inProgress', items: [] };
			live.set(turnId, turn);
		}
		const method = payload.method;
		if (method === 'turn/completed') {
			turn.status = text(nativeTurn.status);
			turn.error = nativeTurn.error;
		}
		if (method === 'turn/plan/updated') turn.plan = list(params.plan).map(object);
		if (method === 'turn/diff/updated') turn.diff = text(params.diff);
		if (method === 'item/started' || method === 'item/completed') {
			const item = object(params.item);
			const id = text(item.id);
			if (!id) continue;
			const at = turn.items.findIndex((current) => current.id === id);
			if (at >= 0) turn.items[at] = { ...item };
			else turn.items.push({ ...item });
			if (method === 'item/completed') completed.add(`${turnId}/${id}`);
		} else if (itemDeltaMethods.has(method)) {
			const id = payload.item_id || text(params.itemId);
			if (!id || completed.has(`${turnId}/${id}`)) continue;
			let item = turn.items.find((current) => current.id === id);
			if (!item) {
				item = {
					id,
					type: method.includes('commandExecution')
						? 'commandExecution'
						: method.includes('reasoning')
							? 'reasoning'
							: method.includes('/plan/')
								? 'plan'
								: 'agentMessage'
				};
				turn.items.push(item);
			}
			const field = method.includes('commandExecution')
				? 'aggregatedOutput'
				: method.includes('reasoning')
					? 'streamedSummary'
					: 'text';
			item[field] = text(item[field]) + text(params.delta);
		}
	}
	for (const [id, liveTurn] of live) {
		const base = turns.get(id);
		if (!base) {
			turns.set(id, liveTurn);
			continue;
		}
		const items = new Map(base.items.map((item) => [text(item.id), item]));
		for (const item of liveTurn.items) {
			const key = text(item.id);
			const existing = items.get(key);
			if (!existing || completed.has(`${id}/${key}`)) {
				items.set(key, item);
				continue;
			}
			const merged = { ...existing, ...item };
			for (const field of ['text', 'aggregatedOutput', 'streamedSummary']) {
				const before = text(existing[field]);
				const after = text(item[field]);
				merged[field] = before.startsWith(after)
					? before
					: after.startsWith(before)
						? after
						: before || after;
			}
			items.set(key, merged);
		}
		turns.set(id, {
			...base,
			...liveTurn,
			status: ['completed', 'interrupted', 'failed'].includes(base.status)
				? base.status
				: liveTurn.status,
			items: [...items.values()]
		});
	}
	const order = history.map((turn) => turn.id);
	const liveOrder = [...live.keys()];
	for (const [index, id] of liveOrder.entries()) {
		if (order.includes(id)) continue;
		const next = liveOrder.slice(index + 1).find((nextID) => order.includes(nextID));
		if (next) order.splice(order.indexOf(next), 0, id);
		else order.push(id);
	}
	return order.map((id) => turns.get(id)!).filter(Boolean);
};

export { object, list, text, label, nativeText, mergeTimeline };
export type { NativeObject, TimelineTurn };
