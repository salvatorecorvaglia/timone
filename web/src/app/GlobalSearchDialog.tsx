import * as Dialog from '@radix-ui/react-dialog';
import { Command, Search, X } from 'lucide-react';
import { lazy, Suspense } from 'react';
import { useNavigate, useSearchParams } from 'react-router-dom';
import { PageBoundary } from '../components/PageBoundary';
import { Loading } from '../components/ui';
import { resourceURL } from '../lib/api';
import { withNamespace } from '../lib/navigation';
import type { Cluster } from '../types';

const GlobalResults = lazy(() =>
  import('../features/GlobalSearch').then((m) => ({ default: m.GlobalResults })),
);

export function GlobalSearchDialog({
  namespace: actualNS,
  cluster,
  open: searchOpen,
  onOpenChange: setSearchOpen,
}: {
  namespace: string;
  cluster: Cluster | undefined;
  open: boolean;
  onOpenChange: (open: boolean) => void;
}) {
  const [params, setParams] = useSearchParams();
  const search = params.get('gq') ?? '';
  const navigate = useNavigate();
  const setSearch = (value: string) =>
    setParams(
      (prev) => {
        const next = new URLSearchParams(prev);
        next.set('gq', value);
        next.delete('goffset');
        return next;
      },
      { replace: true },
    );
  return (
    <Dialog.Root open={searchOpen} onOpenChange={setSearchOpen}>
      <Dialog.Portal>
        <Dialog.Overlay className="search-backdrop" />
        <Dialog.Content className="search-dialog" aria-describedby={undefined}>
          <Dialog.Title className="sr-only">Global resource search</Dialog.Title>
          <div className="search-input">
            <Search size={20} />
            <input
              autoFocus
              placeholder="Search resources across your cluster…"
              aria-label="Search all resources"
              maxLength={200}
              value={search}
              onChange={(e) => setSearch(e.target.value)}
            />
            <button
              className="icon-button"
              aria-label="Close search"
              onClick={() => setSearchOpen(false)}
            >
              <X size={18} />
            </button>
          </div>
          <PageBoundary>
            <Suspense fallback={<Loading />}>
              <GlobalResults
                search={search}
                namespace={actualNS}
                kinds={cluster?.kinds ?? []}
                onSelect={(r) => {
                  navigate(withNamespace(resourceURL(r), actualNS));
                }}
              />
            </Suspense>
          </PageBoundary>
          <div className="search-footer">
            <Command size={12} /> Resources you have permission to read <kbd>esc to close</kbd>
          </div>
        </Dialog.Content>
      </Dialog.Portal>
    </Dialog.Root>
  );
}
