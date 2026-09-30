import { useCallback, useEffect, useState } from "react";
import { fetchBindings, fetchPolicyText, type BindingDTO } from "./api";
import { BindingsPanel } from "./components/BindingsPanel";
import { ToolsPanel } from "./components/ToolsPanel";
import "./App.css";

type Tab = "bindings" | "tools" | "policy";

export default function App() {
  const [tab, setTab] = useState<Tab>("bindings");
  const [bindings, setBindings] = useState<BindingDTO[]>([]);
  const [policyText, setPolicyText] = useState("");
  const [loading, setLoading] = useState(true);
  const [loadError, setLoadError] = useState("");

  const loadBindings = useCallback(async () => {
    setLoadError("");
    try {
      const b = await fetchBindings();
      setBindings(b.bindings ?? []);
    } catch (e) {
      setLoadError(e instanceof Error ? e.message : String(e));
    }
  }, []);

  const loadPolicy = useCallback(async () => {
    try {
      const p = await fetchPolicyText();
      setPolicyText(p.text ?? "");
    } catch (e) {
      setLoadError(e instanceof Error ? e.message : String(e));
    }
  }, []);

  const refreshAll = useCallback(async () => {
    setLoading(true);
    await Promise.all([loadBindings(), loadPolicy()]);
    setLoading(false);
  }, [loadBindings, loadPolicy]);

  useEffect(() => {
    void refreshAll();
  }, [refreshAll]);

  return (
    <div className="app-shell">
      <aside className="sidebar">
        <div className="brand">
          <span className="brand-mark">R</span>
          <div>
            <strong>RBAC 控制台</strong>
            <span className="brand-sub">策略与绑定管理</span>
          </div>
        </div>
        <nav className="nav">
          <button
            type="button"
            className={tab === "bindings" ? "nav-item active" : "nav-item"}
            onClick={() => setTab("bindings")}
          >
            绑定管理
          </button>
          <button
            type="button"
            className={tab === "tools" ? "nav-item active" : "nav-item"}
            onClick={() => setTab("tools")}
          >
            策略试算
          </button>
          <button
            type="button"
            className={tab === "policy" ? "nav-item active" : "nav-item"}
            onClick={() => setTab("policy")}
          >
            策略原文
          </button>
        </nav>
        <p className="sidebar-note">
          修改会写入策略文件。公开 <code>/v1</code> API 仍无鉴权，请勿暴露到公网。
        </p>
      </aside>

      <div className="main-column">
        <header className="topbar">
          <div>
            <h1>
              {tab === "bindings" && "绑定管理"}
              {tab === "tools" && "策略试算"}
              {tab === "policy" && "策略原文"}
            </h1>
            <p className="topbar-sub">
              {tab === "bindings" &&
                "创建、编辑、启用或删除授权边；支持搜索与状态筛选。"}
              {tab === "tools" && "在变更策略前后快速验证 Enforce 与 Reachable。"}
              {tab === "policy" && "当前持久化文件的规范文本（只读展示）。"}
            </p>
          </div>
        </header>

        <main className="content">
          {tab === "bindings" && (
            <BindingsPanel
              bindings={bindings}
              loading={loading}
              error={loadError}
              onRefresh={refreshAll}
            />
          )}
          {tab === "tools" && <ToolsPanel />}
          {tab === "policy" && (
            <section className="card policy-card">
              <pre>{policyText || "（空策略）"}</pre>
            </section>
          )}
        </main>
      </div>
    </div>
  );
}
