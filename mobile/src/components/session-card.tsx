import { useRouter } from 'expo-router';
import { Pressable, StyleSheet, Text, View } from 'react-native';

import { StatusPill } from '@/components/status-pill';
import { sessionPath } from '@/lib/hub/client';
import { nativeSessionLabel } from '@/lib/hub/session-status';
import type { SessionSummary } from '@/lib/hub/types';

type SessionCardProps = {
  session: SessionSummary;
};

const SessionCard = ({ session }: SessionCardProps) => {
  const router = useRouter();
  const { hostId, localId } = sessionPath(session.id);

  return (
    <Pressable
      onPress={() => {
        router.push({
          pathname: '/sessions/[host]/[local]',
          params: { host: hostId, local: localId },
        });
      }}
      style={({ pressed }) => [styles.card, pressed && styles.pressed]}
    >
      <View style={styles.header}>
        <View style={styles.titleBlock}>
          <Text numberOfLines={1} style={styles.title}>
            {session.repo || session.repo_id || session.id}
          </Text>
          <Text numberOfLines={1} style={styles.subtitle}>
            {hostId} · {session.agent}
          </Text>
        </View>
        <StatusPill
          status={
            session.agent === 'codex-cli'
              ? nativeSessionLabel(session.codex)
              : session.status
          }
        />
      </View>

      <View style={styles.meta}>
        <Text numberOfLines={1} style={styles.metaText}>
          <Text style={styles.metaLabel}>branch: </Text>
          {session.branch || '-'}
        </Text>
        <Text numberOfLines={1} style={styles.metaText}>
          <Text style={styles.metaLabel}>created: </Text>
          {new Date(session.created_at).toLocaleString()}
        </Text>
      </View>
    </Pressable>
  );
};

const styles = StyleSheet.create({
  card: {
    gap: 12,
    borderColor: '#dce3ea',
    borderRadius: 8,
    borderWidth: 1,
    backgroundColor: '#ffffff',
    padding: 14,
  },

  pressed: {
    opacity: 0.75,
  },

  header: {
    flexDirection: 'row',
    alignItems: 'flex-start',
    justifyContent: 'space-between',
    gap: 12,
  },

  titleBlock: {
    flex: 1,
    gap: 4,
  },

  title: {
    color: '#101820',
    fontSize: 16,
    fontWeight: '700',
  },

  subtitle: {
    color: '#5c6873',
    fontSize: 13,
    lineHeight: 18,
  },

  meta: {
    gap: 4,
  },

  metaText: {
    color: '#5c6873',
    fontSize: 13,
    lineHeight: 18,
  },

  metaLabel: {
    color: '#8a96a3',
  },
});

export { SessionCard };
