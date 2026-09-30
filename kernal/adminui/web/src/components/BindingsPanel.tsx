import { useMemo, useState } from "react";
import {
  createBinding,
  defaultBinding,
  deleteBinding,
  formatConditions,
  setBindingEnabled,
  updateBinding,
  type BindingDTO,
} from "../api";
import { BindingFormModal } from "./BindingFormModal";

type Props = {
  bindings: BindingDTO[];
  loading: boolean;
  error: string;
  onRefresh: () => Promise<void>;
};

export function BindingsPanel({ bindings, loading, error, onRefresh }: Props) {
  const [query, setQuery] = useState("");
  const [statusFilter, setStatusFilter] = useState<"all" | "on" | "off">("all");
  const [modal, setModal] = useState<{
    open: boolean;
    mode: "create" | "edit";
    data: BindingDTO;
  }>({ open: false, mode: "create", data: defaultBinding() });
  const [actionErr, setActionErr] = useState("");

  const stats = useMemo(() => {
    const total = bindings.length;
    const enabled = bindings.filter((b) => b.enabled).length;
    return { total, enabled, disabled: total - enabled };
  }, [bindings]);

  const filtered = useMemo(() => {
    const q = query.trim().toLowerCase();
    return bindings.filter((b) => {
      if (statusFilter === "on" && !b.enabled) {
        return false;
      }
      if (statusFilter === "off" && b.enabled) {
        return false;
      }
      if (!q) {
        return true;
      }
      const hay = `${b.src} ${b.dst} ${b.scenario}`.toLowerCase();
      return hay.includes(q);
    });
  }, [bindings, query, statusFilter]);

  async function runAction(fn: () => Promise<unknown>) {
    setActionErr("");
    try {
      await fn();
      await onRefresh();
    } catch (e) {
      setActionErr(e instanceof Error ? e.message : String(e));
    }
  }

  function openCreate() {
    setModal({ open: true, mode: "create", data: defaultBinding() });
  }

  function openEdit(b: BindingDTO) {
    setModal({ open: true, mode: "edit", data: { ...b } });
  }

  async function onSubmit(b: BindingDTO) {
    if (modal.mode === "create") {
      await createBinding(b);
    } else {
      await updateBinding(b);
    }
    await onRefresh();
  }

  return (
    <div className="panel">
      <div className="stat-row">
        <div className="stat-card">
          <span className="stat-label">绑定总数</span>
          <strong className="stat-value">{stats.total}</strong>
        </div>
        <div className="stat-card ok">
          <span className="stat-label">已启用</span>
          <strong className="stat-value">{stats.enabled}</strong>
        </div>
        <div className="stat-card muted">
          <span className="stat-label">已停用</span>
          <strong className="stat-value">{stats.disabled}</strong>
        </div>
      </div>

      <div className="toolbar card">
        <div className="toolbar-left">
          <input
            className="search"
            placeholder="搜索源、目标或场景…"
            value={query}
            onChange={(e) => setQuery(e.target.value)}
          />
          <select
            value={statusFilter}
            onChange={(e) =>
              setStatusFilter(e.target.value as "all" | "on" | "off")
            }
          >
            <option value="all">全部状态</option>
            <option value="on">仅启用</option>
            <option value="off">仅停用</option>
          </select>
        </div>
        <div className="toolbar-right">
          <button type="button" className="btn ghost" onClick={() => void onRefresh()}>
            刷新
          </button>
          <button type="button" className="btn primary" onClick={openCreate}>
            新建绑定
          </button>
        </div>
      </div>

      {(error || actionErr) && (
        <p className="err banner">{error || actionErr}</p>
      )}

      <div className="card table-card">
        {loading ? (
          <p className="muted pad">加载中…</p>
        ) : filtered.length === 0 ? (
          <p className="muted pad">暂无匹配的绑定关系。</p>
        ) : (
          <div className="table-wrap">
            <table className="data-table">
              <thead>
                <tr>
                  <th>源</th>
                  <th>目标</th>
                  <th>场景</th>
                  <th>状态</th>
                  <th>条件</th>
                  <th className="col-actions">操作</th>
                </tr>
              </thead>
              <tbody>
                {filtered.map((b) => (
                  <tr key={`${b.src}|${b.dst}|${b.scenario}`} className={!b.enabled ? "row-off" : ""}>
                    <td>
                      <code className="id">{b.src}</code>
                    </td>
                    <td>
                      <code className="id">{b.dst}</code>
                    </td>
                    <td>{b.scenario || "—"}</td>
                    <td>
                      {b.enabled ? (
                        <span className="badge ok">启用</span>
                      ) : (
                        <span className="badge off">停用</span>
                      )}
                    </td>
                    <td className="cond-cell">{formatConditions(b.conditions)}</td>
                    <td className="col-actions">
                      <div className="action-group">
                        <button
                          type="button"
                          className="btn link"
                          onClick={() =>
                            void runAction(async () => {
                              await setBindingEnabled({
                                src: b.src,
                                dst: b.dst,
                                scenario: b.scenario,
                                enabled: !b.enabled,
                              });
                            })
                          }
                        >
                          {b.enabled ? "停用" : "启用"}
                        </button>
                        <button
                          type="button"
                          className="btn link"
                          onClick={() => openEdit(b)}
                        >
                          编辑
                        </button>
                        <button
                          type="button"
                          className="btn link danger"
                          onClick={() => {
                            if (
                              !window.confirm(
                                `确定删除绑定 ${b.src} → ${b.dst}${b.scenario ? `（${b.scenario}）` : ""}？`,
                              )
                            ) {
                              return;
                            }
                            void runAction(async () => {
                              await deleteBinding(
                                b.src,
                                b.dst,
                                b.scenario || undefined,
                              );
                            });
                          }}
                        >
                          删除
                        </button>
                      </div>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
        <footer className="table-foot muted">
          显示 {filtered.length} / {bindings.length} 条
        </footer>
      </div>

      <BindingFormModal
        open={modal.open}
        mode={modal.mode}
        initial={modal.data}
        onClose={() => setModal((m) => ({ ...m, open: false }))}
        onSubmit={onSubmit}
      />
    </div>
  );
}
