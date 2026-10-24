import { expect, it } from 'vitest';
import type { AccessSession, Workspace } from '../types';
import fixtureData from '../test-fixtures.json';

// Direct assignment checks the actual Go-generated fields and types at compile time.
const workspace: Workspace = fixtureData['/api/v1/workspace'];
const session: AccessSession = fixtureData['/api/v1/session'];

it('keeps Go workspace and session responses aligned with their frontend contracts', () => {
  expect(Object.keys(workspace).sort()).toEqual([
    'context',
    'contexts',
    'enabled',
    'message',
    'namespace',
    'revision',
  ]);
  expect(workspace.revision).toBeGreaterThan(0);
  expect(workspace.contexts).toEqual([]);
  expect(session).toMatchObject({
    authenticated: true,
    mode: 'token',
    identity: { method: 'anonymous' },
  });
});
