import { Component, type ReactNode } from 'react';

interface Props {
  children: ReactNode;
  /** Optional custom fallback. Defaults to a minimal error card. */
  fallback?: ReactNode;
  /** If true, the boundary resets when the key prop changes (e.g. on route change). */
  resetOnNavigate?: boolean;
}

interface State {
  hasError: boolean;
  error: Error | null;
}

/**
 * ErrorBoundary — catches render/lifecycle errors in the subtree and shows
 * a fallback instead of crashing the whole application.
 *
 * Usage:
 *   <ErrorBoundary>
 *     <MyComponent />
 *   </ErrorBoundary>
 *
 * React requires this to be a class component.
 */
export class ErrorBoundary extends Component<Props, State> {
  constructor(props: Props) {
    super(props);
    this.state = { hasError: false, error: null };
  }

  static getDerivedStateFromError(error: Error): State {
    return { hasError: true, error };
  }

  componentDidCatch(error: Error, info: { componentStack: string }) {
    console.error('[ErrorBoundary] Caught render error:', error, info.componentStack);
  }

  reset = () => this.setState({ hasError: false, error: null });

  render() {
    if (this.state.hasError) {
      if (this.props.fallback) return this.props.fallback;

      return (
        <div style={{
          display: 'flex',
          flexDirection: 'column',
          alignItems: 'center',
          justifyContent: 'center',
          minHeight: '60vh',
          gap: '16px',
          padding: '40px',
          textAlign: 'center',
        }}>
          <div style={{ fontSize: '48px' }}>⚠️</div>
          <h2 style={{
            fontSize: '20px',
            fontWeight: 600,
            color: 'var(--text-primary, #f1f5f9)',
            margin: 0,
          }}>
            Something went wrong
          </h2>
          <p style={{
            fontSize: '14px',
            color: 'var(--text-muted, #64748b)',
            maxWidth: '480px',
            margin: 0,
            lineHeight: 1.6,
          }}>
            {this.state.error?.message || 'An unexpected error occurred rendering this page.'}
          </p>
          <div style={{ display: 'flex', gap: '12px', marginTop: '8px' }}>
            <button
              onClick={this.reset}
              style={{
                padding: '8px 20px',
                background: 'var(--primary, #06b6d4)',
                border: 'none',
                borderRadius: '6px',
                color: '#fff',
                fontSize: '14px',
                cursor: 'pointer',
                fontWeight: 500,
              }}
            >
              Try Again
            </button>
            <button
              onClick={() => { window.location.href = '/products'; }}
              style={{
                padding: '8px 20px',
                background: 'var(--bg-tertiary, #1e293b)',
                border: '1px solid var(--border-bright, #334155)',
                borderRadius: '6px',
                color: 'var(--text-primary, #f1f5f9)',
                fontSize: '14px',
                cursor: 'pointer',
              }}
            >
              Go Home
            </button>
          </div>
          {process.env.NODE_ENV === 'development' && (
            <pre style={{
              marginTop: '16px',
              padding: '12px 16px',
              background: 'rgba(248,113,113,0.08)',
              border: '1px solid rgba(248,113,113,0.2)',
              borderRadius: '6px',
              fontSize: '11px',
              color: '#f87171',
              textAlign: 'left',
              maxWidth: '640px',
              overflow: 'auto',
              whiteSpace: 'pre-wrap',
              wordBreak: 'break-word',
            }}>
              {this.state.error?.stack}
            </pre>
          )}
        </div>
      );
    }

    return this.props.children;
  }
}

export default ErrorBoundary;
