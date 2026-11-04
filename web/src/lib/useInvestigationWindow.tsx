import { useEffect, useState } from 'react';
import { useSearchParams } from 'react-router-dom';

function localTime(value: string) {
  const t = new Date(value);
  if (!Number.isFinite(t.getTime())) return '';
  return new Date(t.getTime() - t.getTimezoneOffset() * 60000).toISOString().slice(0, 19);
}
export function useInvestigationWindow() {
  const [params, setParams] = useSearchParams();
  const [initial] = useState(() => {
    const end = new Date();
    return { since: new Date(end.getTime() - 3600000).toISOString(), until: end.toISOString() };
  });
  const since = params.get('since') ?? initial.since;
  const until = params.get('until') ?? initial.until;
  useEffect(() => {
    if (params.has('since') && params.has('until')) return;
    setParams(
      (previous) => {
        const next = new URLSearchParams(previous);
        if (!next.has('since')) next.set('since', initial.since);
        if (!next.has('until')) next.set('until', initial.until);
        return next;
      },
      { replace: true },
    );
  }, [params, initial, setParams]);

  const [draftSince, setSince] = useState(localTime(since));
  const [draftUntil, setUntil] = useState(localTime(until));
  useEffect(() => {
    setSince(localTime(since));
    setUntil(localTime(until));
  }, [since, until]);
  const start = new Date(since).getTime();
  const end = new Date(until).getTime();
  const valid =
    Number.isFinite(start) &&
    Number.isFinite(end) &&
    start < end &&
    end - start <= 86400000 &&
    end <= Date.now() + 1000;
  const controls = (
    <form
      className="filterbar"
      onSubmit={(event) => {
        event.preventDefault();
        const a = new Date(draftSince),
          b = new Date(draftUntil);
        if (!Number.isFinite(a.getTime()) || !Number.isFinite(b.getTime())) return;
        const next = new URLSearchParams(params);
        next.set('since', a.toISOString());
        next.set('until', b.toISOString());
        setParams(next);
      }}
    >
      <label>
        From{' '}
        <input
          aria-label="Investigation from"
          required
          type="datetime-local"
          step="1"
          value={draftSince}
          onChange={(e) => setSince(e.target.value)}
        />
      </label>
      <label>
        To{' '}
        <input
          aria-label="Investigation to"
          required
          type="datetime-local"
          step="1"
          value={draftUntil}
          onChange={(e) => setUntil(e.target.value)}
        />
      </label>
      <button className="button" type="submit">
        Apply interval
      </button>
      {!valid && <p role="alert">Select a past interval of up to 24 hours.</p>}
    </form>
  );
  return {
    since,
    until,
    valid,
    controls,
    freezeEnd: () =>
      setParams((previous) => {
        const next = new URLSearchParams(previous);
        next.set('until', new Date().toISOString());
        return next;
      }),
  };
}
