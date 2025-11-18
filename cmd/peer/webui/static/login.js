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

pass.addEventListener("keydown", (evt) => {
  if (evt.key === "Enter") {
    handleLogin();
  }
});
