import axios from "axios";

const client = axios.create({
  baseURL: "/",
  timeout: 20000
});

client.interceptors.request.use((cfg) => {
  const token = localStorage.getItem("token");
  if (token) {
    cfg.headers.Authorization = `Bearer ${token}`;
  }
  return cfg;
});

export async function apiGet(url, params = {}) {
  const { data } = await client.get(url, { params });
  return data;
}

export async function apiPost(url, body = {}) {
  const { data } = await client.post(url, body);
  return data;
}

export async function apiPut(url, body = {}) {
  const { data } = await client.put(url, body);
  return data;
}
