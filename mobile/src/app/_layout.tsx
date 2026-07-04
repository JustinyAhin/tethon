import { Stack } from 'expo-router';
import { StatusBar } from 'expo-status-bar';

import { AuthProvider, useAuth } from '@/lib/hub/auth-context';

export default function RootLayout() {
  return (
    <AuthProvider>
      <RootStack />
      <StatusBar style="auto" />
    </AuthProvider>
  );
}

const RootStack = () => {
  const { status } = useAuth();

  if (status === 'loading') {
    return null;
  }

  const signedIn = status === 'signed-in';

  return (
    <Stack screenOptions={{ headerShown: false }}>
      <Stack.Protected guard={signedIn}>
        <Stack.Screen name="(tabs)" />
        <Stack.Screen
          name="sessions/new"
          options={{
            headerShown: true,
            title: 'Start session',
            headerTintColor: '#1769aa',
            headerStyle: { backgroundColor: '#f7f9fb' },
            headerShadowVisible: false,
          }}
        />
        <Stack.Screen
          name="sessions/[host]/[local]"
          options={{
            headerShown: true,
            title: 'Session',
            headerTintColor: '#1769aa',
            headerStyle: { backgroundColor: '#f7f9fb' },
            headerShadowVisible: false,
          }}
        />
      </Stack.Protected>
      <Stack.Protected guard={!signedIn}>
        <Stack.Screen name="connect" />
      </Stack.Protected>
    </Stack>
  );
};
