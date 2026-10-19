import { Platform, ScrollView, StyleSheet, Text, View } from 'react-native';

import { MarkdownOutput } from '@/components/markdown-output';
import type { TimelineEntry } from '@/lib/hub/codex-timeline';

const CodexTimelineCard = ({ entry }: { entry: TimelineEntry }) => (
  <View
    style={[
      styles.card,
      entry.title === 'You' && styles.user,
      entry.warning && styles.warning,
    ]}
  >
    <Text selectable style={styles.title}>
      {entry.title}
    </Text>
    {entry.detail ? <Text style={styles.detail}>{entry.detail}</Text> : null}

    {entry.markdown ? (
      <MarkdownOutput source={entry.text} />
    ) : entry.text ? (
      <ScrollView
        horizontal={entry.title === 'Changes' || entry.title === 'File changes'}
      >
        <Text
          selectable
          style={[
            styles.body,
            (entry.title === 'Changes' ||
              entry.title === 'File changes' ||
              entry.title === 'Command output') &&
              styles.code,
          ]}
        >
          {entry.text}
        </Text>
      </ScrollView>
    ) : null}
  </View>
);

const styles = StyleSheet.create({
  card: {
    gap: 8,
    padding: 12,
    borderWidth: 1,
    borderColor: '#dce3ea',
    borderRadius: 8,
    backgroundColor: '#fff',
  },

  user: { marginLeft: 24, backgroundColor: '#e8f1fa' },

  warning: { backgroundColor: '#fdf1dc' },

  title: { fontSize: 14, fontWeight: '700', color: '#101820' },

  detail: { fontSize: 12, color: '#5c6873' },

  body: { fontSize: 14, lineHeight: 21, color: '#2f3a43' },

  code: {
    fontFamily: Platform.select({ ios: 'Menlo', default: 'monospace' }),
    fontSize: 12,
    lineHeight: 18,
  },
});

export { CodexTimelineCard };
