export type ConditionDTO = {
  kind: string;
  start?: string;
  end?: string;
};

export type BindingDTO = {
  src: string;
  dst: string;
  scenario: string;
  enabled: boolean;
  conditions: ConditionDTO[];
};

function apiURL(path: string): string {
  const { protocol, host } = window.location;
  return `${protocol}//${host}${path.startsWith("/") ? path : `/${path}`}`;
}

async function api<T>(
  path: string,
  init?: RequestInit,
): Promise<{ data: T; status: number }> {
  const res = await fetch(apiURL(path), {
    credentials: "same-origin",
    ...init,
  });
  if (res.status === 401) {
    throw new Error("未授权，请检查浏览器中的 Basic 认证账号密码");
  }
  if (res.status === 204) {
    return { data: undefined as T, status: res.status };
  }
  const text = await res.text();
  let data: { error?: string } | null = null;
  try {
    data = text ? JSON.parse(text) : null;
  } catch {
    data = { error: text };
  }
  if (!res.ok) {
    throw new Error((data && data.error) || res.statusText);
  }
  return { data: data as T, status: res.status };
}

export function fetchBindings() {
  return api<{ bindings: BindingDTO[] }>("/api/bindings").then((r) => r.data);
}

export function createBinding(body: BindingDTO) {
  return api<BindingDTO>("/api/bindings", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(body),
  }).then((r) => r.data);
}

export function updateBinding(body: BindingDTO) {
  return api<BindingDTO>("/api/bindings", {
    method: "PUT",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(body),
  }).then((r) => r.data);
}

export function setBindingEnabled(payload: {
  src: string;
  dst: string;
  scenario?: string;
  enabled: boolean;
}) {
  return api<{ ok: boolean }>("/api/bindings/enabled", {
    method: "PATCH",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({
      src: payload.src,
      dst: payload.dst,
      scenario: payload.scenario ?? "",
      enabled: payload.enabled,
    }),
  }).then((r) => r.data);
}

export function deleteBinding(src: string, dst: string, scenario?: string) {
  const q = new URLSearchParams({ src, dst });
  if (scenario) {
    q.set("scenario", scenario);
  }
  return api<void>(`/api/bindings?${q.toString()}`, { method: "DELETE" });
}

export function fetchPolicyText() {
  return api<{ text: string }>("/api/policy").then((r) => r.data);
}

export function postEnforce(body: {
  subject: string;
  target: string;
  scenarios?: string[];
}) {
  return api<{ allow: boolean }>("/api/enforce", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(body),
  }).then((r) => r.data);
}

export function fetchReachable(subject: string, scenario?: string) {
  const q = new URLSearchParams({ subject });
  if (scenario) {
    q.append("scenario", scenario);
  }
  return api<{ subject: string; reachable: string[] }>(
    `/api/reachable?${q.toString()}`,
  ).then((r) => r.data);
}

export function formatConditions(conditions: ConditionDTO[] | undefined): string {
  if (!conditions?.length) {
    return "无条件";
  }
  return conditions
    .map((x) => {
      if ((x.kind || "").toUpperCase() === "ALL") {
        return "无条件";
      }
      return `时间 ${x.start || "—"} ~ ${x.end || "—"}`;
    })
    .join("；");
}

export function defaultBinding(): BindingDTO {
  return {
    src: "",
    dst: "",
    scenario: "",
    enabled: true,
    conditions: [{ kind: "ALL" }],
  };
}
