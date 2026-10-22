import { useEffect, useState } from 'react';
import { useQueryClient } from '@tanstack/react-query';
import { useNavigate } from 'react-router-dom';
import { shouldInvalidate, type Invalidation } from './updates';

export function useLiveUpdates() {
  const qc = useQueryClient();
  const navigate = useNavigate();
  const [connection, setConnection] = useState('Connecting');
  useEffect(() => {
    let timer: ReturnType<typeof setTimeout> | undefined;
    let disposed = false;
    let refreshing = false;
    let dirty = false;
    let pending: Invalidation = { scopes: [] };
    const schedule = () => {
      if (disposed || refreshing || timer || !dirty) return;
      timer = setTimeout(() => void flush(), 1000);
    };
    const flush = async () => {
      timer = undefined;
      if (disposed) return;
      const batch = pending;
      pending = { scopes: [] };
      dirty = false;
      refreshing = true;
      try {
        await qc.invalidateQueries(
          { predicate: (query) => shouldInvalidate(String(query.queryKey[0]), batch) },
          { cancelRefetch: false },
        );
      } finally {
        refreshing = false;
        schedule();
      }
    };
    const changed = () => {
      disposed = true;
      clearTimeout(timer);
      stream.close();
      void qc.cancelQueries().then(() => {
        qc.clear();
        navigate('/', { replace: true });
        window.location.reload();
      });
    };
    window.addEventListener('timone:workspace-changed', changed);
    const stream = new EventSource('/api/v1/stream');
    const invalidate = (event: Event) => {
      if (disposed) return;
      dirty = true;
      try {
        const value = JSON.parse((event as MessageEvent<string>).data) as Invalidation;
        if (value.full || !Array.isArray(value.scopes)) pending.full = true;
        else if (
          value.scopes.length > 256 ||
          value.scopes.some((s) => typeof s.kind !== 'string' || typeof s.namespace !== 'string')
        )
          pending.full = true;
        else pending.scopes = [...(pending.scopes ?? []), ...value.scopes];
        if ((pending.scopes?.length ?? 0) > 256) {
          pending.full = true;
          pending.scopes = [];
        }
      } catch {
        pending.full = true;
      }
      setConnection('Updates live');
      schedule();
    };
    stream.addEventListener('invalidate', invalidate);
    stream.onopen = () => setConnection('Updates live');
    stream.onerror = () => setConnection('Reconnecting');
    return () => {
      disposed = true;
      window.removeEventListener('timone:workspace-changed', changed);
      stream.close();
      clearTimeout(timer);
    };
  }, [qc, navigate]);
  return connection;
}
