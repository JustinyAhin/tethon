import { useCallback, useEffect, useRef, useState } from 'react';
import Ionicons from '@expo/vector-icons/Ionicons';
import { useRouter } from 'expo-router';
import {
  ActivityIndicator,
  FlatList,
  Pressable,
  RefreshControl,
  StyleSheet,
  Text,
  View,
} from 'react-native';
import { SafeAreaView } from 'react-native-safe-area-context';

import { SessionCard } from '@/components/session-card';
import { useSessions } from '@/lib/hub/use-sessions';

export default function Sessions() {
  const router = useRouter();
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
          <Pressable
            accessibilityLabel="Start session"
            onPress={() => {
              router.push('../sessions/new');
            }}
            style={({ pressed }) => [
              styles.addButton,
              pressed && styles.pressed,
            ]}
          >
            <Ionicons color="#ffffff" name="add" size={22} />
          </Pressable>
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
                <View style={styles.emptyState}>
                  <Text style={styles.emptyText}>No sessions yet.</Text>
                  <Pressable
                    onPress={() => {
                      router.push('../sessions/new');
                    }}
                    style={({ pressed }) => [
                      styles.emptyButton,
                      pressed && styles.pressed,
                    ]}
                  >
                    <Ionicons color="#ffffff" name="add" size={18} />
                    <Text style={styles.emptyButtonText}>Start session</Text>
                  </Pressable>
                </View>
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
  addButton: {
    width: 40,
    height: 40,
    alignItems: 'center',
    justifyContent: 'center',
    borderRadius: 8,
    backgroundColor: '#1769aa',
  },
  pressed: {
    opacity: 0.75,
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
  emptyState: {
    alignItems: 'center',
    gap: 14,
  },
  emptyButton: {
    minHeight: 40,
    flexDirection: 'row',
    alignItems: 'center',
    justifyContent: 'center',
    gap: 8,
    borderRadius: 8,
    backgroundColor: '#1769aa',
    paddingHorizontal: 14,
  },
  emptyButtonText: {
    color: '#ffffff',
    fontSize: 14,
    fontWeight: '700',
  },
});
