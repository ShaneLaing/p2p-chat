const API_BASE = "http://127.0.0.1:8089";
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
    const res = await fetch(`${API_BASE}/${path}`, {
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
    localStorage.setItem("token", data.token);
    localStorage.setItem("username", data.username);
    localStorage.setItem("auth_api", API_BASE);
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
