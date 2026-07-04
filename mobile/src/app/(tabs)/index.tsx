import { useCallback, useEffect, useRef, useState } from 'react';
import {
  ActivityIndicator,
  FlatList,
  RefreshControl,
  StyleSheet,
  Text,
  View,
} from 'react-native';
import { SafeAreaView } from 'react-native-safe-area-context';

import { SessionCard } from '@/components/session-card';
import { useSessions } from '@/lib/hub/use-sessions';

export default function Sessions() {
  const { error, loading, refresh, sessions } = useSessions();
  const [refreshing, setRefreshing] = useState(false);
  const mounted = useRef(true);

  useEffect(() => {
    return () => {
      mounted.current = false;
    };
  }, []);

  const refreshList = useCallback(async () => {
    setRefreshing(true);

    try {
      await refresh();
    } finally {
      if (mounted.current) {
        setRefreshing(false);
      }
    }
  }, [refresh]);

  return (
    <SafeAreaView style={styles.safeArea}>
      <View style={styles.container}>
        <View style={styles.header}>
          <View style={styles.heading}>
            <Text style={styles.title}>Sessions</Text>
          </View>
        </View>

        {loading ? (
          <View style={styles.centerState}>
            <ActivityIndicator />
          </View>
        ) : error !== null && sessions.length === 0 ? (
          <View style={styles.centerState}>
            <Text style={styles.error}>{error}</Text>
          </View>
        ) : (
          <>
            {error !== null ? (
              <View style={styles.errorBanner}>
                <Text style={styles.errorBannerText}>{error}</Text>
              </View>
            ) : null}

            <FlatList
              contentContainerStyle={[
                styles.listContent,
                sessions.length === 0 && styles.emptyList,
              ]}
              data={sessions}
              keyExtractor={(session) => session.id}
              ListEmptyComponent={
                <Text style={styles.emptyText}>
                  No sessions yet. Start sessions from the web app for now.
                </Text>
              }
              refreshControl={
                <RefreshControl onRefresh={refreshList} refreshing={refreshing} />
              }
              renderItem={({ item }) => <SessionCard session={item} />}
            />
          </>
        )}
      </View>
    </SafeAreaView>
  );
}

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
  errorBanner: {
    borderColor: '#f3bfbb',
    borderRadius: 8,
    borderWidth: 1,
    backgroundColor: '#fbeae9',
    marginBottom: 12,
    paddingHorizontal: 12,
    paddingVertical: 9,
  },
  errorBannerText: {
    color: '#b42318',
    fontSize: 13,
    lineHeight: 18,
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
    lineHeight: 21,
    maxWidth: 260,
    textAlign: 'center',
  },
});
