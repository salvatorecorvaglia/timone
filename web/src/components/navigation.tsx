import { Link, useSearchParams, type LinkProps } from 'react-router-dom';

import { withNamespace } from '../lib/navigation';
export function ScopedLink({ to, ...props }: LinkProps) {
  const [params] = useSearchParams();
  return (
    <Link
      {...props}
      to={typeof to === 'string' ? withNamespace(to, params.get('namespace') ?? '') : to}
    />
  );
}
