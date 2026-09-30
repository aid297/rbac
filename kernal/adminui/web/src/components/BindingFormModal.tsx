import { useEffect, useState } from "react";
import type { BindingDTO } from "../api";

type Props = {
  open: boolean;
  mode: "create" | "edit";
  initial: BindingDTO;
  onClose: () => void;
  onSubmit: (b: BindingDTO) => Promise<void>;
};

export function BindingFormModal({
  open,
  mode,
  initial,
  onClose,
  onSubmit,
}: Props) {
  const [form, setForm] = useState<BindingDTO>(initial);
  const [condKind, setCondKind] = useState<"ALL" | "TIME">("ALL");
  const [timeStart, setTimeStart] = useState("");
  const [timeEnd, setTimeEnd] = useState("");
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState("");

  useEffect(() => {
    if (!open) {
      return;
    }
    setForm(initial);
    const first = initial.conditions?.[0];
    const kind =
      first && (first.kind || "").toUpperCase() === "TIME" ? "TIME" : "ALL";
    setCondKind(kind);
    setTimeStart(first?.start ?? "");
    setTimeEnd(first?.end ?? "");
    setErr("");
  }, [open, initial]);

  if (!open) {
    return null;
  }

  async function handleSubmit(e: React.FormEvent) {
    e.preventDefault();
    setErr("");
    setBusy(true);
    try {
      const conditions =
        condKind === "ALL"
          ? [{ kind: "ALL" as const }]
          : [
              {
                kind: "TIME" as const,
                ...(timeStart.trim() ? { start: timeStart.trim() } : {}),
                ...(timeEnd.trim() ? { end: timeEnd.trim() } : {}),
              },
            ];
      await onSubmit({
        ...form,
        src: form.src.trim(),
        dst: form.dst.trim(),
        scenario: form.scenario.trim(),
        conditions,
      });
      onClose();
    } catch (ex) {
      setErr(ex instanceof Error ? ex.message : String(ex));
    } finally {
      setBusy(false);
    }
  }

  const identityLocked = mode === "edit";

  return (
    <div className="modal-backdrop" onClick={onClose} role="presentation">
      <div
        className="modal"
        onClick={(e) => e.stopPropagation()}
        role="dialog"
        aria-labelledby="binding-modal-title"
      >
        <header className="modal-head">
          <h3 id="binding-modal-title">
            {mode === "create" ? "新建绑定" : "编辑绑定"}
          </h3>
          <button type="button" className="icon-btn" onClick={onClose} aria-label="关闭">
            ×
          </button>
        </header>
        <form onSubmit={(e) => void handleSubmit(e)}>
          <div className="form-grid">
            <label>
              源（主体侧）
              <input
                required
                disabled={identityLocked}
                value={form.src}
                onChange={(e) => setForm({ ...form, src: e.target.value })}
                placeholder="用户或角色 ID"
              />
            </label>
            <label>
              目标（权限侧）
              <input
                required
                disabled={identityLocked}
                value={form.dst}
                onChange={(e) => setForm({ ...form, dst: e.target.value })}
                placeholder="权限或资源 ID"
              />
            </label>
            <label>
              场景
              <input
                disabled={identityLocked}
                value={form.scenario}
                onChange={(e) => setForm({ ...form, scenario: e.target.value })}
                placeholder="留空表示通用边"
              />
            </label>
            <label className="checkbox-row">
              <input
                type="checkbox"
                checked={form.enabled}
                onChange={(e) => setForm({ ...form, enabled: e.target.checked })}
              />
              {mode === "create" ? "创建后立即启用" : "启用此绑定"}
            </label>
          </div>

          <fieldset className="fieldset">
            <legend>生效条件</legend>
            <div className="cond-tabs">
              <button
                type="button"
                className={condKind === "ALL" ? "tab active" : "tab"}
                onClick={() => setCondKind("ALL")}
              >
                无条件
              </button>
              <button
                type="button"
                className={condKind === "TIME" ? "tab active" : "tab"}
                onClick={() => setCondKind("TIME")}
              >
                时间窗口
              </button>
            </div>
            {condKind === "TIME" && (
              <div className="form-grid">
                <label>
                  开始（RFC3339，可选）
                  <input
                    value={timeStart}
                    onChange={(e) => setTimeStart(e.target.value)}
                    placeholder="2026-01-01T00:00:00Z"
                  />
                </label>
                <label>
                  结束（RFC3339，可选）
                  <input
                    value={timeEnd}
                    onChange={(e) => setTimeEnd(e.target.value)}
                    placeholder="2027-01-01T00:00:00Z"
                  />
                </label>
              </div>
            )}
          </fieldset>

          {err && <p className="err">{err}</p>}

          <footer className="modal-foot">
            <button type="button" className="btn ghost" onClick={onClose}>
              取消
            </button>
            <button type="submit" className="btn primary" disabled={busy}>
              {busy ? "保存中…" : "保存"}
            </button>
          </footer>
        </form>
      </div>
    </div>
  );
}
