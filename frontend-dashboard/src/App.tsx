import { useCallback, useEffect, useState } from "react";
import { ChainBar } from "./components/ChainBar";
import { Sidebar } from "./components/Sidebar";
import type { ChainStatusDTO } from "./api/types";
import { chainFilterVisible, useChainFilter } from "./hooks/useChainFilter";
import { OverviewView } from "./views/OverviewView";
import { PipelineView } from "./views/PipelineView";
import { IngestionView } from "./views/IngestionView";
import { ExecutionsView } from "./views/ExecutionsView";
import { PositionsView } from "./views/PositionsView";
import { ActivityView } from "./views/ActivityView";
import { DQView } from "./views/DQView";
import { GateView } from "./views/GateView";
import { ModeView } from "./views/ModeView";
import { SafetyView } from "./views/SafetyView";
import { ConfigsView } from "./views/ConfigsView";
import type { DashboardView } from "./types/views";
import { NAV_SECTIONS, VIEW_TITLES } from "./types/views";

function allViews(): DashboardView[] {
  return NAV_SECTIONS.flatMap((s) => s.items.map((i) => i.view));
}

function ViewBody({
  view,
  active,
  chainFilter,
  onNavigate,
  onChainStatuses,
}: {
  view: DashboardView;
  active: boolean;
  chainFilter: ReturnType<typeof useChainFilter>;
  onNavigate: (view: DashboardView) => void;
  onChainStatuses: (statuses: ChainStatusDTO[]) => void;
}) {
  if (!active) {
    return null;
  }

  switch (view) {
    case "overview":
      return (
        <OverviewView
          chain={chainFilter.chain}
          market={chainFilter.market}
          active={active}
          onNavigate={onNavigate}
          onChainStatuses={onChainStatuses}
        />
      );
    case "pipeline":
      return (
        <PipelineView
          chain={chainFilter.chain}
          market={chainFilter.market}
          active={active}
        />
      );
    case "ingestion":
      return <IngestionView active={active} />;
    case "executions":
      return <ExecutionsView active={active} />;
    case "positions":
      return (
        <PositionsView
          chain={chainFilter.chain}
          market={chainFilter.market}
          active={active}
        />
      );
    case "activity":
      return <ActivityView chain={chainFilter.chain} active={active} />;
    case "dq":
      return (
        <DQView chain={chainFilter.chain} market={chainFilter.market} active={active} />
      );
    case "gate":
      return <GateView chain={chainFilter.chain} active={active} />;
    case "mode":
      return (
        <ModeView
          chain={chainFilter.chain}
          market={chainFilter.market}
          active={active}
        />
      );
    case "safety":
      return <SafetyView active={active} />;
    case "configs":
      return <ConfigsView active={active} />;
    default:
      return null;
  }
}

export default function App() {
  const [activeView, setActiveView] = useState<DashboardView>("overview");
  const [chainStatuses, setChainStatuses] = useState<ChainStatusDTO[]>([]);
  const chainFilter = useChainFilter();
  const showChainFilter = chainFilterVisible(activeView);

  const setView = useCallback((view: DashboardView) => {
    setActiveView(view);
    window.scrollTo(0, 0);
  }, []);

  const handleChainStatuses = useCallback((statuses: ChainStatusDTO[]) => {
    setChainStatuses(statuses);
  }, []);

  useEffect(() => {
    const onKeyDown = (e: KeyboardEvent) => {
      if (!e.altKey || e.ctrlKey || e.metaKey) {
        return;
      }
      const views = allViews();
      const idx = views.indexOf(activeView);
      if (idx < 0) {
        return;
      }
      if (e.key === "ArrowDown") {
        e.preventDefault();
        setView(views[(idx + 1) % views.length]);
      } else if (e.key === "ArrowUp") {
        e.preventDefault();
        setView(views[(idx - 1 + views.length) % views.length]);
      }
    };
    window.addEventListener("keydown", onKeyDown);
    return () => window.removeEventListener("keydown", onKeyDown);
  }, [activeView, setView]);

  return (
    <div className="app">
      <Sidebar
        activeView={activeView}
        onViewChange={setView}
        chainFilterVisible={showChainFilter}
        chain={chainFilter.chain}
        onChainChange={chainFilter.setChain}
      />

      <main className="main">
        <ChainBar
          visible={showChainFilter}
          chain={chainFilter.chain}
          market={chainFilter.market}
          markets={chainFilter.markets}
          marketDisabled={chainFilter.marketDisabled}
          chainStatuses={activeView === "overview" ? chainStatuses : undefined}
          onChainChange={chainFilter.setChain}
          onMarketChange={chainFilter.setMarket}
        />

        {allViews().map((view) => (
          <section
            key={view}
            className={`view-panel${view === activeView ? " active" : ""}`}
            data-view={view}
            id={`view-${view}`}
            aria-hidden={view !== activeView}
          >
            <header className="page-header">
              <div>
                <h2>
                  {VIEW_TITLES[view].title}
                  {showChainFilter && view === activeView ? (
                    <span className={`chain-tag ${chainFilter.chain}`}>{chainFilter.chain}</span>
                  ) : null}
                </h2>
                <p className="subtitle">{VIEW_TITLES[view].subtitle}</p>
              </div>
            </header>

            <ViewBody
              view={view}
              active={view === activeView}
              chainFilter={chainFilter}
              onNavigate={setView}
              onChainStatuses={handleChainStatuses}
            />
          </section>
        ))}
      </main>
    </div>
  );
}
