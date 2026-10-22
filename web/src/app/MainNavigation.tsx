import { NavLink } from 'react-router-dom';
import {
  Activity,
  AlertTriangle,
  Box,
  GitBranch,
  Layers3,
  LayoutDashboard,
  Network,
  Plug,
  ShieldCheck,
  ShipWheel,
} from 'lucide-react';

export const navigation = [
  {
    group: 'WORKSPACE',
    items: [
      { path: '/', name: 'Overview', icon: LayoutDashboard },
      { path: '/issues', name: 'Issues', icon: AlertTriangle },
      { path: '/applications', name: 'Applications', icon: Layers3 },
      { path: '/topology', name: 'Topology', icon: Network },
    ],
  },
  {
    group: 'CLUSTER',
    items: [
      { path: '/resources', name: 'Resources', icon: Box },
      { path: '/custom-resources', name: 'Custom resources', icon: Layers3 },
      { path: '/events', name: 'Events & Timeline', icon: Activity },
      { path: '/metrics', name: 'Metrics', icon: Activity },
    ],
  },
  {
    group: 'OPERATIONS',
    items: [
      { path: '/helm', name: 'Helm', icon: ShipWheel },
      { path: '/gitops', name: 'GitOps', icon: GitBranch },
      { path: '/audit', name: 'Security & Audit', icon: ShieldCheck },
      { path: '/integrations', name: 'Integrations', icon: Plug },
    ],
  },
];
export function MainNavigation({
  namespace,
  issues,
  onNavigate,
}: {
  namespace: string;
  issues: number;
  onNavigate: () => void;
}) {
  return (
    <nav aria-label="Main navigation">
      {navigation.map((g) => (
        <div className="nav-group" key={g.group}>
          <div className="nav-label">{g.group}</div>
          {g.items.map((i) => (
            <NavLink
              key={i.path}
              to={i.path + (namespace ? `?namespace=${encodeURIComponent(namespace)}` : '')}
              end={i.path === '/'}
              onClick={onNavigate}
            >
              <i.icon size={17} />
              <span>{i.name}</span>
              {i.path === '/issues' && issues > 0 && <span className="nav-count">{issues}</span>}
              {i.path === '/topology' && <span className="nav-new">GRAPH</span>}
            </NavLink>
          ))}
        </div>
      ))}
    </nav>
  );
}
