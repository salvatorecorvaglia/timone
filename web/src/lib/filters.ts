import { useSearchParams } from 'react-router-dom';
export function useFilters() {
  const [p, set] = useSearchParams();
  return {
    p,
    search: p.get('search') ?? '',
    status: p.get('status') ?? '',
    update: (key: string, value: string) => {
      const n = new URLSearchParams(p);
      if (value) n.set(key, value);
      else n.delete(key);
      n.delete('offset');
      set(n, { replace: true });
    },
  };
}
