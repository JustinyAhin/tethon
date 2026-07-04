import { useEffect, useState } from 'react';
import {
  ActivityIndicator,
  FlatList,
  Pressable,
  StyleSheet,
  Text,
  View,
} from 'react-native';
import { SafeAreaView } from 'react-native-safe-area-context';

import { fetchHosts, HubAuthError, HubRequestError } from '@/lib/hub/client';
import { useAuth } from '@/lib/hub/auth-context';
import type { HostSummary } from '@/lib/hub/types';

export default function Hosts() {
  const { hubUrl, invalidate, signOut, status } = useAuth();
  const [hosts, setHosts] = useState<HostSummary[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    if (status !== 'signed-in') {
      return;
    }

    let cancelled = false;

    const loadHosts = async () => {
      setLoading(true);
      setError(null);

      try {
        const response = await fetchHosts();

        if (!cancelled) {
          setHosts(response.hosts);
        }
      } catch (caught) {
        if (cancelled) {
          return;
        }

        if (caught instanceof HubAuthError) {
          await invalidate();
          return;
        }

        setError(toHomeErrorMessage(caught));
      } finally {
        if (!cancelled) {
          setLoading(false);
        }
      }
    };

    void loadHosts();

    return () => {
      cancelled = true;
    };
  }, [invalidate, status]);

  return (
    <SafeAreaView style={styles.safeArea}>
      <View style={styles.container}>
        <View style={styles.header}>
          <View style={styles.heading}>
            <Text style={styles.title}>tethon</Text>
            <Text style={styles.hubUrl}>{hubUrl}</Text>
          </View>
          <Pressable onPress={signOut} style={styles.disconnectButton}>
            <Text style={styles.disconnectText}>Disconnect</Text>
          </Pressable>
        </View>

        {loading ? (
          <View style={styles.centerState}>
            <ActivityIndicator />
          </View>
        ) : error !== null ? (
          <View style={styles.centerState}>
            <Text style={styles.error}>{error}</Text>
          </View>
        ) : (
          <FlatList
            contentContainerStyle={[
              styles.listContent,
              hosts.length === 0 && styles.emptyList,
            ]}
            data={hosts}
            keyExtractor={(host) => host.host_id}
            ListEmptyComponent={
              <Text style={styles.emptyText}>No hosts registered.</Text>
            }
            renderItem={({ item }) => <HostRow host={item} />}
          />
        )}
      </View>
    </SafeAreaView>
  );
}

const HostRow = ({ host }: { host: HostSummary }) => (
  <View style={styles.hostRow}>
    <View style={styles.hostMain}>
      <Text style={styles.hostName}>{host.name}</Text>
      <Text style={styles.hostDetail}>{host.host_id}</Text>
      <Text style={styles.hostDetail}>{host.url}</Text>
    </View>
    <View
      style={[styles.statusPill, host.online ? styles.online : styles.offline]}
    >
      <Text
        style={[
          styles.statusText,
          host.online ? styles.onlineText : styles.offlineText,
        ]}
      >
        {host.online ? 'Online' : 'Offline'}
      </Text>
    </View>
  </View>
);

const toHomeErrorMessage = (error: unknown): string => {
  if (error instanceof HubRequestError) {
    if (error.status === undefined) {
      return 'Could not reach hub';
    }

    return `Hub returned HTTP ${error.status}`;
  }

  return 'Could not load hosts';
};

const styles = StyleSheet.create({
  safeArea: {
    flex: 1,
    backgroundColor: '#f7f9fb',
  },
  container: {
    flex: 1,
    paddingHorizontal: 20,
  },
  header: {
    flexDirection: 'row',
    alignItems: 'flex-start',
    justifyContent: 'space-between',
    gap: 16,
    paddingBottom: 20,
    paddingTop: 16,
  },
  heading: {
    flex: 1,
    gap: 6,
  },
  title: {
    color: '#101820',
    fontSize: 28,
    fontWeight: '700',
  },
  hubUrl: {
    color: '#5c6873',
    fontSize: 14,
    lineHeight: 20,
  },
  disconnectButton: {
    minHeight: 36,
    justifyContent: 'center',
    borderColor: '#cdd6df',
    borderRadius: 8,
    borderWidth: 1,
    paddingHorizontal: 12,
  },
  disconnectText: {
    color: '#2f3a43',
    fontSize: 14,
    fontWeight: '600',
  },
  centerState: {
    flex: 1,
    alignItems: 'center',
    justifyContent: 'center',
    padding: 24,
  },
  error: {
    color: '#b42318',
    fontSize: 15,
    textAlign: 'center',
  },
  listContent: {
    gap: 12,
    paddingBottom: 32,
  },
  emptyList: {
    flexGrow: 1,
    alignItems: 'center',
    justifyContent: 'center',
  },
  emptyText: {
    color: '#5c6873',
    fontSize: 15,
  },
  hostRow: {
    flexDirection: 'row',
    alignItems: 'flex-start',
    justifyContent: 'space-between',
    gap: 12,
    borderColor: '#dce3ea',
    borderRadius: 8,
    borderWidth: 1,
    backgroundColor: '#ffffff',
    padding: 14,
  },
  hostMain: {
    flex: 1,
    gap: 4,
  },
  hostName: {
    color: '#101820',
    fontSize: 16,
    fontWeight: '700',
  },
  hostDetail: {
    color: '#5c6873',
    fontSize: 13,
    lineHeight: 18,
  },
  statusPill: {
    borderRadius: 999,
    paddingHorizontal: 10,
    paddingVertical: 5,
  },
  online: {
    backgroundColor: '#e7f6ed',
  },
  offline: {
    backgroundColor: '#f1f3f5',
  },
  statusText: {
    fontSize: 12,
    fontWeight: '700',
  },
  onlineText: {
    color: '#087443',
  },
  offlineText: {
    color: '#5c6873',
  },
});
