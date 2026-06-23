import { useCallback, useEffect, useState, type KeyboardEvent } from "react";
import type { ChainId } from "../hooks/useChainFilter";
import type { DashboardView } from "../types/views";
import { NAV_SECTIONS } from "../types/views";
import { ChainSwitch } from "./ChainSwitch";

const STORAGE_SIDEBAR = "sidebar-collapsed";

type SidebarProps = {
  activeView: DashboardView;
  onViewChange: (view: DashboardView) => void;
  chainFilterVisible: boolean;
  chain: ChainId;
  onChainChange: (chain: ChainId) => void;
};

export function Sidebar({
  activeView,
  onViewChange,
  chainFilterVisible,
  chain,
  onChainChange,
}: SidebarProps) {
  const [collapsed, setCollapsed] = useState(() => {
    try {
      return localStorage.getItem(STORAGE_SIDEBAR) === "1";
    } catch {
      return false;
    }
  });

  useEffect(() => {
    document.body.classList.toggle("sidebar-collapsed", collapsed);
    try {
      localStorage.setItem(STORAGE_SIDEBAR, collapsed ? "1" : "0");
    } catch {
      /* ignore */
    }
    return () => {
      document.body.classList.remove("sidebar-collapsed");
    };
  }, [collapsed]);

  const toggleCollapsed = useCallback(() => {
    setCollapsed((c) => !c);
  }, []);

  const onNavKeyDown = useCallback(
    (e: KeyboardEvent, view: DashboardView) => {
      if (e.key === "Enter" || e.key === " ") {
        e.preventDefault();
        onViewChange(view);
      }
    },
    [onViewChange],
  );

  return (
    <aside className="sidebar" id="sidebar">
      <div className="sidebar-header">
        <div className="brand">
          <div className="brand-icon" aria-hidden>
            CS
          </div>
          <div className="brand-text">
            <h1>Sniping Bot</h1>
            <p>Operator dashboard</p>
          </div>
        </div>
        <button
          type="button"
          className="sidebar-toggle"
          title={collapsed ? "Expand sidebar" : "Collapse sidebar"}
          aria-label={collapsed ? "Expand sidebar" : "Collapse sidebar"}
          aria-expanded={!collapsed}
          onClick={toggleCollapsed}
        >
          ☰
        </button>
      </div>

      <nav className="nav" aria-label="Main">
        {NAV_SECTIONS.map((section) => (
          <div key={section.title}>
            <div className="nav-section">{section.title}</div>
            {section.items.map((item) => (
              <button
                key={item.view}
                type="button"
                className={item.view === activeView ? "active" : ""}
                aria-current={item.view === activeView ? "page" : undefined}
                onClick={() => onViewChange(item.view)}
                onKeyDown={(e) => onNavKeyDown(e, item.view)}
              >
                <span className="nav-icon" aria-hidden>
                  {item.icon}
                </span>
                <span className="label">{item.label}</span>
                {item.badge ? <span className="badge">{item.badge}</span> : null}
              </button>
            ))}
          </div>
        ))}
      </nav>

      <div
        className={`sidebar-extra${chainFilterVisible ? "" : " chain-filter-hidden"}`}
        id="sidebar-quick-chain"
      >
        <label className="sidebar-extra-label">Quick chain</label>
        <ChainSwitch
          activeChain={chain}
          onSelect={onChainChange}
          ariaLabel="Chain filter sidebar"
          compact
        />
      </div>

      <div className="sidebar-tip">
        <strong>Tip</strong>
        <br />
        Chain filter appears only on views that use per-chain data.
      </div>
    </aside>
  );
}
