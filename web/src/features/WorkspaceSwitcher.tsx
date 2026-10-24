import { useState } from 'react';
import { request, useAPI } from '../lib/api';
import { ErrorState, Loading } from '../components/ui';

import type { Workspace } from '../types';

export function WorkspaceSwitcher({ close }: { close: () => void }) {
  const data = useAPI<Workspace>('/api/v1/workspace');
  const [context, setContext] = useState<string | null>(null);
  const [namespace, setNamespace] = useState<string | null>(null);
  const [confirm, setConfirm] = useState(false);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<Error | null>(null);
  if (data.isPending) return <Loading />;
  if (data.error) return <ErrorState error={data.error} />;
  const workspace = data.data;
  if (!workspace.enabled) return <p>{workspace.message}</p>;
  return (
    <form
      className="workspace-switch-form"
      onSubmit={(e) => {
        e.preventDefault();
        setBusy(true);
        setError(null);
        void request<Workspace>('/api/v1/workspace', {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({
            context: context ?? workspace.context,
            namespace: namespace ?? workspace.namespace,
            revision: workspace.revision,
            confirm,
          }),
        })
          .then(close)
          .catch((e) => {
            setError(e);
            void data.refetch();
          })
          .finally(() => setBusy(false));
      }}
    >
      <label>
        Context
        <select
          aria-label="Kubeconfig context"
          value={context ?? workspace.context}
          onChange={(e) => setContext(e.target.value)}
        >
          {workspace.contexts.map((c) => (
            <option key={c}>{c}</option>
          ))}
        </select>
      </label>
      <label>
        Namespace ceiling
        <input
          aria-label="Workspace namespace ceiling"
          placeholder="All namespaces"
          maxLength={63}
          value={namespace ?? workspace.namespace}
          onChange={(e) => setNamespace(e.target.value)}
        />
      </label>
      <p>{workspace.message}</p>
      <p>
        Empty namespace ceiling means all namespaces allowed by the selected Kubernetes credentials.
      </p>
      <label>
        <input type="checkbox" checked={confirm} onChange={(e) => setConfirm(e.target.checked)} />{' '}
        Confirm workspace reset
      </label>
      {error && <ErrorState error={error} />}
      <button className="button primary" disabled={busy || !confirm}>
        {busy ? 'Switching…' : 'Switch workspace'}
      </button>
    </form>
  );
}
