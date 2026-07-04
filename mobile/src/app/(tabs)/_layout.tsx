import { Tabs } from 'expo-router';
import Ionicons from '@expo/vector-icons/Ionicons';

export default function TabsLayout() {
  return (
    <Tabs
      screenOptions={{
        headerShown: false,
        tabBarActiveTintColor: '#1769aa',
        tabBarInactiveTintColor: '#5c6873',
        tabBarStyle: {
          backgroundColor: '#ffffff',
          borderTopColor: '#dce3ea',
        },
      }}
    >
      <Tabs.Screen
        name="index"
        options={{
          title: 'Sessions',
          tabBarIcon: ({ color, size }) => (
            <Ionicons color={color} name="chatbubbles-outline" size={size} />
          ),
        }}
      />
      <Tabs.Screen
        name="hosts"
        options={{
          title: 'Hosts',
          tabBarIcon: ({ color, size }) => (
            <Ionicons color={color} name="server-outline" size={size} />
          ),
        }}
      />
    </Tabs>
  );
}
