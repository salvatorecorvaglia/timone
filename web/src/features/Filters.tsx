import { Search } from 'lucide-react';

export function Filters({
  search,
  status,
  onSearch,
  onStatus,
  placeholder = 'Search…',
  info = false,
}: {
  search: string;
  status: string;
  onSearch: (v: string) => void;
  onStatus: (v: string) => void;
  placeholder?: string;
  info?: boolean;
}) {
  return (
    <div className="filterbar">
      <div className="search-field">
        <Search size={15} />
        <input
          value={search}
          aria-label={placeholder}
          placeholder={placeholder}
          maxLength={200}
          onChange={(e) => onSearch(e.target.value)}
        />
      </div>
      <select aria-label="Severity" value={status} onChange={(e) => onStatus(e.target.value)}>
        <option value="">All severities</option>
        <option value="critical">Critical</option>
        <option value="warning">Warning</option>
        {info && <option value="info">Info</option>}
      </select>
    </div>
  );
}
