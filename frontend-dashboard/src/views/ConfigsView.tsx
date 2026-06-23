import { useCallback, useEffect, useMemo, useState } from "react";
import { dashboardApi } from "../api/client";
import type { ConfigManifestEntryDTO } from "../api/types";
import { ViewState } from "../components/ViewState";
import { usePolling } from "../hooks/usePolling";
import { formatEventTime } from "../utils/format";

type ConfigsViewProps = {
  active: boolean;
};

export function ConfigsView({ active }: ConfigsViewProps) {
  const fetchConfigs = useCallback(() => dashboardApi.getConfigs(), []);

  const { data, loading, error } = usePolling(fetchConfigs, [], { enabled: active });

  return (
    <ViewState loading={loading} error={error} empty={data === null}>
      {data ? <ConfigsBrowser entries={data} /> : null}
    </ViewState>
  );
}

function ConfigsBrowser({ entries }: { entries: ConfigManifestEntryDTO[] }) {
  const sorted = useMemo(
    () => [...entries].sort((a, b) => a.filename.localeCompare(b.filename)),
    [entries],
  );

  const [selected, setSelected] = useState<string>("");

  useEffect(() => {
    if (sorted.length === 0) {
      setSelected("");
      return;
    }
    if (!selected || !sorted.some((e) => e.filename === selected)) {
      setSelected(sorted[0].filename);
    }
  }, [sorted, selected]);

  const active = sorted.find((e) => e.filename === selected);

  if (sorted.length === 0) {
    return <p className="hint view-section">No YAML files found in config manifest.</p>;
  }

  return (
    <div className="config-browser">
      <div className="config-list" role="tablist" aria-label="Config files">
        {sorted.map((entry) => (
          <button
            key={entry.filename}
            type="button"
            role="tab"
            aria-selected={entry.filename === selected}
            className={entry.filename === selected ? "active" : ""}
            onClick={() => setSelected(entry.filename)}
          >
            {entry.filename}
          </button>
        ))}
      </div>

      <div className="config-viewer">
        {active ? (
          <>
            <div className="config-viewer-header">
              <span>
                <code>{active.filename}</code>
              </span>
              <span className="hint">config/{active.filename}</span>
            </div>
            <pre>{renderManifestDetail(active)}</pre>
            <div className="config-note">
              Read-only manifest — values are not exposed via API. Full files live in the repo;
              secrets use <code className="mono">${"{"}ENV_VAR{"}"}</code> placeholders only.
            </div>
          </>
        ) : null}
      </div>
    </div>
  );
}

function renderManifestDetail(entry: ConfigManifestEntryDTO): string {
  const lines = [
    `# config/${entry.filename}`,
    `# sha256 prefix: ${entry.sha256_prefix}`,
    `# last modified: ${formatEventTime(entry.last_modified)}`,
    "",
    "# top-level keys:",
    ...(entry.top_level_keys?.length
      ? entry.top_level_keys.map((k) => `#   - ${k}`)
      : ["#   (none parsed)"]),
    "",
    "# File contents are not served by the dashboard API.",
    "# Edit in config/ and restart processes to apply changes.",
  ];
  return lines.join("\n");
}
