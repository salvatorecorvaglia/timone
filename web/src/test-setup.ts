import '@testing-library/jest-dom/vitest';
import { cleanup } from '@testing-library/react';
import { afterEach, vi } from 'vitest';
class Observer {
  observe() {}
  unobserve() {}
  disconnect() {}
}
vi.stubGlobal('ResizeObserver', Observer);
vi.stubGlobal(
  'EventSource',
  class extends EventTarget {
    onopen: unknown;
    onerror: unknown;
    close() {}
  },
);
afterEach(() => {
  cleanup();
  vi.restoreAllMocks();
  localStorage.clear();
});
