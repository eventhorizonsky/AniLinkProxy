import { computed, ref } from "vue";

const tokenRef = ref(localStorage.getItem("token") || "");
const roleRef = ref(localStorage.getItem("role") || "");
const userRef = ref(parseUser(localStorage.getItem("user")));

function parseUser(raw) {
  if (!raw) return null;
  try {
    return JSON.parse(raw);
  } catch {
    return null;
  }
}

export function setAuth(data) {
  const token = data?.token || "";
  const user = data?.user || null;
  const role = user?.role || "";
  localStorage.setItem("token", token);
  localStorage.setItem("role", role);
  localStorage.setItem("user", JSON.stringify(user || {}));
  tokenRef.value = token;
  roleRef.value = role;
  userRef.value = user;
}

export function clearAuth() {
  localStorage.removeItem("token");
  localStorage.removeItem("role");
  localStorage.removeItem("user");
  tokenRef.value = "";
  roleRef.value = "";
  userRef.value = null;
}

export function syncAuthFromStorage() {
  tokenRef.value = localStorage.getItem("token") || "";
  roleRef.value = localStorage.getItem("role") || "";
  userRef.value = parseUser(localStorage.getItem("user"));
}

export const authState = {
  token: tokenRef,
  role: roleRef,
  user: userRef,
  isAuthed: computed(() => Boolean(tokenRef.value))
};
