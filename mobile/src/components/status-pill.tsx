import { StyleSheet, Text, View } from 'react-native';

type StatusPillProps = {
  status: string;
};

const statusStyles = {
  running: {
    backgroundColor: '#e7f6ed',
    color: '#087443',
  },
  awaiting_approval: {
    backgroundColor: '#fdf1dc',
    color: '#9a6700',
  },
  error: {
    backgroundColor: '#fbeae9',
    color: '#b42318',
  },
  default: {
    backgroundColor: '#f1f3f5',
    color: '#5c6873',
  },
};

export const StatusPill = ({ status }: StatusPillProps) => {
  const colors =
    statusStyles[status as keyof typeof statusStyles] ?? statusStyles.default;

  return (
    <View style={[styles.pill, { backgroundColor: colors.backgroundColor }]}>
      <Text style={[styles.text, { color: colors.color }]}>{status}</Text>
    </View>
  );
};

const styles = StyleSheet.create({
  pill: {
    borderRadius: 999,
    paddingHorizontal: 10,
    paddingVertical: 5,
  },
  text: {
    fontSize: 12,
    fontWeight: '700',
  },
});
