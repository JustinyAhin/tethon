import { Component, type PropsWithChildren, type ReactNode } from 'react';
import { Linking, Platform, StyleSheet, Text } from 'react-native';
import { EnrichedMarkdownText } from 'react-native-enriched-markdown';
import type { MarkdownStyle } from 'react-native-enriched-markdown';

type MarkdownOutputProps = {
  source: string;
};

type ErrorBoundaryState = {
  failed: boolean;
};

class MarkdownErrorBoundary extends Component<
  PropsWithChildren<MarkdownOutputProps>,
  ErrorBoundaryState
> {
  state: ErrorBoundaryState = { failed: false };

  static getDerivedStateFromError(): ErrorBoundaryState {
    return { failed: true };
  }

  render(): ReactNode {
    if (this.state.failed) {
      return <Text style={styles.fallback}>{this.props.source}</Text>;
    }

    return this.props.children;
  }
}

const monospaceFont = Platform.select({ ios: 'Menlo', default: 'monospace' });

const markdownStyle: MarkdownStyle = {
  paragraph: {
    color: '#2f3a43',
    fontSize: 14,
    lineHeight: 21,
  },
  h1: {
    color: '#101820',
    fontSize: 20,
    fontWeight: '700',
    lineHeight: 26,
    marginBottom: 8,
  },
  h2: {
    color: '#101820',
    fontSize: 18,
    fontWeight: '700',
    lineHeight: 24,
    marginBottom: 8,
  },
  h3: {
    color: '#101820',
    fontSize: 16,
    fontWeight: '700',
    lineHeight: 22,
    marginBottom: 6,
  },
  code: {
    backgroundColor: '#f1f3f5',
    color: '#2f3a43',
    fontFamily: monospaceFont,
    fontSize: 13,
  },
  codeBlock: {
    backgroundColor: '#f1f3f5',
    borderRadius: 6,
    color: '#2f3a43',
    fontFamily: monospaceFont,
    fontSize: 13,
    lineHeight: 19,
    padding: 10,
  },
  blockquote: {
    borderColor: '#cdd6df',
    borderWidth: 3,
    color: '#5c6873',
    fontSize: 14,
    gapWidth: 10,
    lineHeight: 21,
  },
  link: {
    color: '#1769aa',
    underline: true,
  },
  list: {
    color: '#2f3a43',
    fontSize: 14,
    gapWidth: 8,
    lineHeight: 21,
    markerColor: '#5c6873',
  },
};

export const MarkdownOutput = ({ source }: MarkdownOutputProps) => (
  <MarkdownErrorBoundary source={source}>
    <EnrichedMarkdownText
      flavor="github"
      markdown={source}
      markdownStyle={markdownStyle}
      onLinkPress={({ url }) => {
        void Linking.openURL(url);
      }}
    />
  </MarkdownErrorBoundary>
);

const styles = StyleSheet.create({
  fallback: {
    color: '#2f3a43',
    fontSize: 14,
    lineHeight: 21,
  },
});
