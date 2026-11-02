import type { Issue } from '../types';
export function RunbookLinks({ issue }: { issue: Issue }) {
  const links = (issue.runbooks ?? []).filter((link) => {
    try {
      const u = new URL(link.url);
      return u.protocol === 'https:' && !u.username && !u.password;
    } catch {
      return false;
    }
  });
  return (
    <div className="investigation-section">
      <h3>Diagnostic rules & runbooks</h3>
      <p className="mono">{issue.ruleIDs?.join(', ') || 'Rule identifiers unavailable'}</p>
      {links.length ? (
        <ul>
          {links.map((link) => (
            <li key={link.id}>
              <a href={link.url} target="_blank" rel="noopener noreferrer">
                {link.title}
              </a>
            </li>
          ))}
        </ul>
      ) : (
        <p className="muted">
          No matching runbook configured. Administrators can map rule IDs and resource scopes using
          TIMONE_RUNBOOKS_FILE.
        </p>
      )}
    </div>
  );
}
