import { useEffect, useRef, useState } from 'react';
import { request } from '../lib/api';
import { ErrorState, Panel, Status } from '../components/ui';
import type { AccessReview } from '../types';
export function EffectiveAuthorization(props: { namespace: string; serviceAccount?: string }) {
  return (
    <AuthorizationInvestigation
      key={`${props.namespace}/${props.serviceAccount ?? ''}`}
      {...props}
    />
  );
}
function AuthorizationInvestigation({
  namespace,
  serviceAccount,
}: {
  namespace: string;
  serviceAccount?: string;
}) {
  const [kind, setKind] = useState('Pod');
  const [verb, setVerb] = useState('get');
  const [name, setName] = useState('');
  const [ns, setNS] = useState(namespace);
  const [result, setResult] = useState<AccessReview | null>(null);
  const [error, setError] = useState<Error | null>(null);
  const [busy, setBusy] = useState(false);
  const controller = useRef<AbortController | null>(null);
  useEffect(() => () => controller.current?.abort(), []);
  const [subject, setSubject] = useState(false);
  return (
    <Panel
      title="Live effective authorization"
      subtitle="Ask the Kubernetes authorizer about the active identity or a selected ServiceAccount."
    >
      <form
        className="filterbar"
        onSubmit={(e) => {
          e.preventDefault();
          controller.current?.abort();
          controller.current = new AbortController();
          setBusy(true);
          setResult(null);
          setError(null);
          const q = new URLSearchParams({
            kind,
            verb,
            name,
            namespace: ns,
            ...(subject && serviceAccount
              ? { serviceAccount, serviceAccountNamespace: namespace }
              : {}),
          });
          void request<AccessReview>(`/api/v1/authorization?${q}`, {
            signal: controller.current.signal,
          })
            .then(setResult)
            .catch(setError)
            .finally(() => setBusy(false));
        }}
      >
        <label>
          Resource
          <select
            aria-label="Authorization resource"
            value={kind}
            onChange={(e) => setKind(e.target.value)}
          >
            {[
              'Pod',
              'Deployment',
              'StatefulSet',
              'Service',
              'ConfigMap',
              'Secret',
              'ServiceAccount',
              'Role',
              'RoleBinding',
            ].map((k) => (
              <option key={k}>{k}</option>
            ))}
          </select>
        </label>
        <label>
          Verb
          <select
            aria-label="Authorization verb"
            value={verb}
            onChange={(e) => setVerb(e.target.value)}
          >
            {['get', 'list', 'watch', 'create', 'update', 'patch', 'delete'].map((v) => (
              <option key={v}>{v}</option>
            ))}
          </select>
        </label>
        <label>
          Namespace
          <input
            aria-label="Authorization namespace"
            required
            value={ns}
            maxLength={63}
            onChange={(e) => setNS(e.target.value)}
          />
        </label>
        <label>
          Name (optional)
          <input
            aria-label="Authorization resource name"
            value={name}
            maxLength={253}
            onChange={(e) => setName(e.target.value)}
          />
        </label>
        {serviceAccount && (
          <label>
            <input
              type="checkbox"
              checked={subject}
              onChange={(e) => setSubject(e.target.checked)}
            />{' '}
            Assess ServiceAccount {namespace}/{serviceAccount}
          </label>
        )}
        <button className="button primary" disabled={busy}>
          {busy ? 'Checking…' : 'Check effective access'}
        </button>
      </form>
      {error && <ErrorState error={error} />}
      {result && (
        <div role="status">
          <Status
            value={
              result.state === 'allowed'
                ? 'healthy'
                : result.state === 'denied'
                  ? 'critical'
                  : 'unknown'
            }
            label={result.state}
          />
          <p>
            {result.identity} · {result.mode === 'demo' ? 'SIMULATED' : 'Kubernetes authorizer'} ·{' '}
            {new Date(result.observedAt).toLocaleString()}
          </p>
          <p>
            Reviewed: {result.query.verb} {result.query.kind} {result.query.namespace}/
            {result.query.name || '*'}
          </p>
          <p>{result.message}</p>
        </div>
      )}
      <p className="footnote">
        SelfSubjectAccessReview uses the active Kubernetes credentials. ServiceAccount assessment
        requires permission to create SubjectAccessReview and to read that ServiceAccount. A
        forbidden review is unavailable evidence, not a denied resource permission. Reviews do not
        change resources or grant access; admission and subsequent policy changes can still block an
        operation.
      </p>
    </Panel>
  );
}
