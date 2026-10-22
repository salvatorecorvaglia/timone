import { Component, type ReactNode } from 'react';
import { ErrorState } from './ui';

export class PageBoundary extends Component<{ children: ReactNode }, { error: Error | null }> {
  state = { error: null as Error | null };
  static getDerivedStateFromError(error: Error) {
    return { error };
  }
  render() {
    return this.state.error ? (
      <ErrorState error={this.state.error} retry={() => window.location.reload()} />
    ) : (
      this.props.children
    );
  }
}
