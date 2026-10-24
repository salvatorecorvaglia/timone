import type { Ref } from 'react';
import { NavLink } from 'react-router-dom';
import { ArrowUpRight, ChevronsUpDown, CircleHelp, Settings2 } from 'lucide-react';
import { Logo } from '../components/Logo';
import { ScopedLink as Link } from '../components/navigation';
import { VERSION } from '../lib/version';
import { MainNavigation } from './MainNavigation';

export function Sidebar({
  sidebar,
  narrow,
  mobile,
  closeMobile,
  namespace,
  issues,
}: {
  sidebar: Ref<HTMLElement>;
  narrow: boolean;
  mobile: boolean;
  closeMobile: () => void;
  namespace: string;
  issues: number | null;
}) {
  return (
    <aside
      id="main-navigation"
      ref={sidebar}
      inert={narrow && !mobile}
      className={`sidebar ${mobile ? 'open' : ''}`}
    >
      <Link to="/" className="brand">
        <Logo />
        Timone<span className="version">{VERSION}</span>
      </Link>
      <button className="button mobile-menu" onClick={closeMobile}>
        Close navigation
      </button>
      <div className="workspace-label">
        <span className="workspace-avatar">T</span>
        <div>
          Timone workspace<small>Cluster operations</small>
        </div>
        <ChevronsUpDown size={14} />
      </div>
      <MainNavigation namespace={namespace} issues={issues} onNavigate={closeMobile} />
      <div className="sidebar-bottom">
        <NavLink to="/settings" onClick={closeMobile}>
          <Settings2 size={17} />
          Settings
        </NavLink>
        <a
          href="https://github.com/kubernetes/website/tree/main/content/en/docs"
          target="_blank"
          rel="noreferrer"
        >
          <CircleHelp size={17} />
          Kubernetes docs
          <ArrowUpRight size={13} />
        </a>
        <div className="sidebar-footer">
          <span className="status-dot healthy" />
          Timone v{VERSION}
          <span>READ ONLY</span>
        </div>
      </div>
    </aside>
  );
}
