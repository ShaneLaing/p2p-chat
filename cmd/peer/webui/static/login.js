const WORKSPACES = {
  mesh: {
    auth: "http://127.0.0.1:8089",
    bootstrap: "http://127.0.0.1:8000",
  },
  lab: {
    auth: "http://127.0.0.1:8090",
    bootstrap: "http://127.0.0.1:8100",
  },
};

const workspaceSelect = document.getElementById("workspace");
if (workspaceSelect) {
  const stored = localStorage.getItem("workspace");
  if (stored && WORKSPACES[stored]) {
    workspaceSelect.value = stored;
  }
}
const API_BASE = () => WORKSPACES[workspaceSelect?.value || "mesh"].auth;
const user = document.getElementById("user");
const pass = document.getElementById("pass");
const msg = document.getElementById("msg");
const dbBanner = document.getElementById("db-banner");

// Track whether the auth database is reachable so we can show the friendly banner
// and skip futile login attempts while persistence is disabled.
const dbState = { healthy: true };
const FRIENDLY_DB_MESSAGE = "Auth server running, but database is disabled. Login data will not be persistent.";

const toggleDbBanner = () => {
  if (!dbBanner) return;
  if (dbState.healthy) {
    dbBanner.classList.add("hidden");
  } else {
    dbBanner.textContent = FRIENDLY_DB_MESSAGE;
    dbBanner.classList.remove("hidden");
  }
};

const updateHealthState = async () => {
  try {
    const res = await fetch(`${API_BASE()}/healthz`, { cache: "no-store" });
    dbState.healthy = res.ok;
  } catch (err) {
    console.warn("healthz check failed", err);
    dbState.healthy = false;
  }
  toggleDbBanner();
  return dbState.healthy;
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
    // Refresh health before making the request so the UI reacts instantly in DB-less mode.
    const healthy = await updateHealthState();
    if (!healthy) {
      msg.textContent = FRIENDLY_DB_MESSAGE;
      return;
    }
    const res = await fetch(`${API_BASE()}/${path}`, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(payload),
    });
    if (!res.ok) {
      if (res.status === 503) {
        dbState.healthy = false;
        toggleDbBanner();
        msg.textContent = FRIENDLY_DB_MESSAGE;
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
    const workspace = WORKSPACES[workspaceSelect?.value || "mesh"];
    localStorage.setItem("token", data.token);
    localStorage.setItem("username", data.username);
    localStorage.setItem("auth_api", workspace.auth);
    localStorage.setItem("bootstrap_url", workspace.bootstrap);
    localStorage.setItem("workspace", workspaceSelect?.value || "mesh");
    window.location.href = "/chat";
  } catch (err) {
    console.error(err);
    msg.textContent = "Network error";
  }
}

loginBtn.addEventListener("click", handleLogin);
registerBtn.addEventListener("click", handleRegister);

// When operators flip between workspaces we re-run the health probe so the banner stays accurate.
workspaceSelect?.addEventListener("change", () => {
  updateHealthState();
});

pass.addEventListener("keydown", (evt) => {
  if (evt.key === "Enter") {
    handleLogin();
  }
});

// Run the initial probe at load to surface DB-less mode right away.
updateHealthState();
