import { useState } from "react";
import { fetchReachable, postEnforce } from "../api";

export function ToolsPanel() {
  const [enforceSubject, setEnforceSubject] = useState("");
  const [enforceTarget, setEnforceTarget] = useState("");
  const [enforceScenario, setEnforceScenario] = useState("");
  const [enforceResult, setEnforceResult] = useState<"allow" | "deny" | null>(
    null,
  );
  const [enforceHint, setEnforceHint] = useState("");
  const [enforceError, setEnforceError] = useState("");

  const [reachSubject, setReachSubject] = useState("");
  const [reachScenario, setReachScenario] = useState("");
  const [reachList, setReachList] = useState<string[] | null>(null);
  const [reachHint, setReachHint] = useState("");
  const [reachError, setReachError] = useState("");

  async function onEnforce() {
    setEnforceError("");
    setEnforceResult(null);
    setEnforceHint("");
    try {
      const body: {
        subject: string;
        target: string;
        scenarios?: string[];
      } = {
        subject: enforceSubject.trim(),
        target: enforceTarget.trim(),
      };
      const sc = enforceScenario.trim();
      if (sc) {
        body.scenarios = [sc];
      }
      const data = await postEnforce(body);
      setEnforceResult(data.allow ? "allow" : "deny");
      if (!data.allow && !sc) {
        setEnforceHint("带场景的绑定在留空场景时不会匹配；若预期允许，请在场景框填写绑定使用的场景后重试。");
      }
    } catch (e) {
      setEnforceError(e instanceof Error ? e.message : String(e));
    }
  }

  async function onReachable() {
    setReachError("");
    setReachList(null);
    setReachHint("");
    try {
      const sc = reachScenario.trim() || undefined;
      const data = await fetchReachable(reachSubject.trim(), sc);
      const list = data.reachable ?? [];
      setReachList(list);
      const subject = reachSubject.trim();
      const noEdges =
        list.length === 0 || (list.length === 1 && list[0] === subject);
      if (noEdges && !sc) {
        setReachHint("未发现授权边或仅有主体自身。带场景的绑定在留空场景时不会匹配，可在场景框填写后重试。");
      }
    } catch (e) {
      setReachError(e instanceof Error ? e.message : String(e));
    }
  }

  return (
    <div className="panel split">
      <section className="card">
        <header className="card-head">
          <h2>权限判定</h2>
          <p>模拟 Enforce：给定主体与目标，判断是否允许访问。</p>
        </header>
        <div className="form-row">
          <label>
            主体
            <input
              value={enforceSubject}
              onChange={(e) => setEnforceSubject(e.target.value)}
              placeholder="用户或角色 ID"
            />
          </label>
          <label>
            目标
            <input
              value={enforceTarget}
              onChange={(e) => setEnforceTarget(e.target.value)}
              placeholder="权限 ID"
            />
          </label>
          <label>
            场景
            <input
              value={enforceScenario}
              onChange={(e) => setEnforceScenario(e.target.value)}
              placeholder="留空仅匹配通用绑定"
            />
          </label>
          <button type="button" className="btn primary" onClick={() => void onEnforce()}>
            判定
          </button>
        </div>
        {enforceResult === "allow" && (
          <p className="result">
            <span className="pill ok">允许</span>
          </p>
        )}
        {enforceResult === "deny" && (
          <p className="result">
            <span className="pill no">拒绝</span>
            {enforceHint && <span className="muted hint">{enforceHint}</span>}
          </p>
        )}
        {enforceError && <p className="err">{enforceError}</p>}
      </section>

      <section className="card">
        <header className="card-head">
          <h2>可达目标</h2>
          <p>列出主体在当前策略下可到达的全部目标节点。</p>
        </header>
        <div className="form-row">
          <label>
            主体
            <input
              value={reachSubject}
              onChange={(e) => setReachSubject(e.target.value)}
              placeholder="用户或角色 ID"
            />
          </label>
          <label>
            场景
            <input
              value={reachScenario}
              onChange={(e) => setReachScenario(e.target.value)}
              placeholder="留空仅匹配通用绑定"
            />
          </label>
          <button type="button" className="btn primary" onClick={() => void onReachable()}>
            查询
          </button>
        </div>
        {reachList !== null && (
          <div className="chip-list">
            {reachList.length === 0 ? (
              <span className="muted">（无）</span>
            ) : (
              reachList.map((id) => (
                <span className="chip" key={id}>
                  {id}
                </span>
              ))
            )}
            {reachHint && <span className="muted hint">{reachHint}</span>}
          </div>
        )}
        {reachError && <p className="err">{reachError}</p>}
      </section>
    </div>
  );
}
