const WORKSPACES = {
  mesh: {
    auth: "http://127.0.0.1:8089",
    bootstrap: "http://127.0.0.1:8000",
  },
  lab: {
    auth: "http://172.18.105.183:8180",
    bootstrap: "http://172.18.105.183:8000",
  },
};

const HEALTH_TIMEOUT_MS = 300;
const STORAGE_KEYS = {
  lastEndpoint: "lastHealthyAuthEndpoint",
  lastWorkspace: "lastHealthyWorkspace",
};
const STATELESS_WARNING = "Stateless mode: Account data will NOT persist after restart.";
const UNAVAILABLE_WARNING = "Auth server unreachable. Login may fail until it recovers.";

const workspaceSelect = document.getElementById("workspace");
const resolveWorkspace = (key) => (WORKSPACES[key] ? key : "mesh");
if (workspaceSelect) {
  const stored = resolveWorkspace(localStorage.getItem("workspace") || workspaceSelect.value || "mesh");
  workspaceSelect.value = stored;
}

const currentWorkspace = () => resolveWorkspace(workspaceSelect?.value || localStorage.getItem("workspace") || "mesh");
const API_BASE = () => WORKSPACES[currentWorkspace()].auth;

const user = document.getElementById("user");
const pass = document.getElementById("pass");
const msg = document.getElementById("msg");
const warningMessage = document.getElementById("warning-message");
const recoveryPanel = document.getElementById("recovery-panel");
const switchEndpointBtn = document.getElementById("switch-endpoint");
const retryLink = document.getElementById("retry-workspace");

const dbState = {
  healthy: true,
  dbEnabled: true,
  lastResponse: null,
};

const showWarning = (text) => {
  if (!warningMessage) return;
  warningMessage.textContent = text;
  warningMessage.classList.remove("hidden");
};

const hideWarning = () => {
  warningMessage?.classList.add("hidden");
};

const updateRecoveryButtonState = () => {
  if (!switchEndpointBtn) return;
  const lastWorkspace = localStorage.getItem(STORAGE_KEYS.lastWorkspace);
  if (lastWorkspace && WORKSPACES[lastWorkspace]) {
    switchEndpointBtn.disabled = false;
    switchEndpointBtn.title = `Switch to ${lastWorkspace}`;
  } else {
    switchEndpointBtn.disabled = true;
    switchEndpointBtn.title = "No known healthy workspace yet";
  }
};

const showRecoveryPanel = () => {
  recoveryPanel?.classList.remove("hidden");
  updateRecoveryButtonState();
};

const hideRecoveryPanel = () => {
  recoveryPanel?.classList.add("hidden");
};

const persistLastHealthyWorkspace = () => {
  const workspace = currentWorkspace();
  const workspaceConfig = WORKSPACES[workspace];
  localStorage.setItem(STORAGE_KEYS.lastWorkspace, workspace);
  localStorage.setItem(STORAGE_KEYS.lastEndpoint, workspaceConfig.auth);
  updateRecoveryButtonState();
};

const fetchHealth = async () => {
  const controller = new AbortController();
  const timeoutId = setTimeout(() => controller.abort(), HEALTH_TIMEOUT_MS);
  try {
    return await fetch(`${API_BASE()}/healthz`, {
      cache: "no-store",
      signal: controller.signal,
    });
  } finally {
    clearTimeout(timeoutId);
  }
};

const handleHealthResult = (ok, payload) => {
  dbState.healthy = ok;
  if (payload && typeof payload.dbEnabled === "boolean") {
    dbState.dbEnabled = payload.dbEnabled;
  } else if (ok) {
    dbState.dbEnabled = true;
  }
  dbState.lastResponse = payload || null;
  if (ok) {
    hideRecoveryPanel();
    if (dbState.dbEnabled) {
      hideWarning();
    } else {
      showWarning(STATELESS_WARNING);
    }
    persistLastHealthyWorkspace();
  } else {
    showRecoveryPanel();
    if (payload?.dbEnabled === false) {
      showWarning(STATELESS_WARNING);
    } else {
      showWarning(UNAVAILABLE_WARNING);
    }
  }
};

const updateHealthState = async () => {
  try {
    const res = await fetchHealth();
    let payload = null;
    try {
      payload = await res.json();
    } catch (err) {
      payload = null;
    }
    handleHealthResult(res.ok, payload);
    return { ok: res.ok, payload };
  } catch (err) {
    console.warn("healthz check failed", err);
    handleHealthResult(false, null);
    return { ok: false, error: err };
  }
};

const loginBtn = document.getElementById("login-btn");
const registerBtn = document.getElementById("register-btn");

const handleLogin = async () => {
  await authenticate("login");
};

const handleRegister = async () => {
  await authenticate("register");
};

async function authenticate(path) {
  msg.textContent = "";
  const payload = {
    username: user.value.trim(),
    password: pass.value,
  };
  if (!payload.username || !payload.password) {
    msg.textContent = "Please enter username and password";
    return;
  }
  try {
    const res = await fetch(`${API_BASE()}/${path}`, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(payload),
    });
    if (!res.ok) {
      if (res.status === 503) {
        updateHealthState();
        const text = await res.text();
        msg.textContent = text || STATELESS_WARNING;
        return;
      }
      const text = await res.text();
      msg.textContent = text || "Request failed";
      return;
    }
    if (path === "register") {
      msg.textContent = "Registration successful. Please login.";
      pass.value = "";
      return;
    }
    const data = await res.json();
    const workspaceKey = currentWorkspace();
    const workspaceConfig = WORKSPACES[workspaceKey];
    localStorage.setItem("token", data.token);
    localStorage.setItem("username", data.username);
    localStorage.setItem("auth_api", workspaceConfig.auth);
    localStorage.setItem("bootstrap_url", workspaceConfig.bootstrap);
    localStorage.setItem("workspace", workspaceKey);
    window.location.href = "/chat";
  } catch (err) {
    console.error(err);
    msg.textContent = "Network error";
    showRecoveryPanel();
  }
}

loginBtn.addEventListener("click", handleLogin);
registerBtn.addEventListener("click", handleRegister);

workspaceSelect?.addEventListener("change", () => {
  localStorage.setItem("workspace", currentWorkspace());
  updateHealthState();
});

switchEndpointBtn?.addEventListener("click", () => {
  const lastWorkspace = localStorage.getItem(STORAGE_KEYS.lastWorkspace);
  if (lastWorkspace && WORKSPACES[lastWorkspace]) {
    if (workspaceSelect) {
      workspaceSelect.value = lastWorkspace;
    }
    localStorage.setItem("workspace", lastWorkspace);
    updateHealthState();
    msg.textContent = `Switched to ${lastWorkspace} workspace.`;
  } else {
    msg.textContent = "No healthy workspace cached yet.";
  }
});

retryLink?.addEventListener("click", (evt) => {
  evt.preventDefault();
  updateHealthState();
});

pass.addEventListener("keydown", (evt) => {
  if (evt.key === "Enter") {
    handleLogin();
  }
});

updateRecoveryButtonState();
updateHealthState();
