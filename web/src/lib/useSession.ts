import { useEffect, useState } from 'react';
import { useQueryClient } from '@tanstack/react-query';
import { request } from './api';
import type { AccessSession } from '../types';

export function useSession() {
  const [authMode, setAuthMode] = useState('token');
  const [authorized, setAuthorized] = useState<boolean | null>(null);
  const [error, setError] = useState<Error | null>(null);
  const qc = useQueryClient();
  useEffect(() => {
    const controller = new AbortController();
    void request<AccessSession>('/api/v1/session', { signal: controller.signal })
      .then((session) => {
        if (controller.signal.aborted) return;
        setAuthMode(session.mode ?? 'token');
        setAuthorized(session.authenticated);
      })
      .catch((e) => {
        if (!controller.signal.aborted) setError(e);
      });
    const listener = () => {
      setAuthorized(false);
      qc.clear();
    };
    window.addEventListener('timone:unauthorized', listener);
    return () => {
      controller.abort();
      window.removeEventListener('timone:unauthorized', listener);
    };
  }, [qc]);
  const authorize = () => {
    qc.clear();
    setAuthorized(true);
  };
  return { authMode, authorized, error, authorize };
}
