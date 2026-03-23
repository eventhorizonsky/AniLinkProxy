import LoginView from "./views/LoginView.vue";
import RegisterView from "./views/RegisterView.vue";
import HomeView from "./views/HomeView.vue";
import KeysView from "./views/KeysView.vue";
import StatsView from "./views/StatsView.vue";
import RiskView from "./views/RiskView.vue";
import AdminView from "./views/AdminView.vue";

export const routes = [
  { path: "/login", component: LoginView },
  { path: "/register", component: RegisterView },
  { path: "/", component: HomeView, meta: { requiresAuth: true } },
  { path: "/keys", component: KeysView, meta: { requiresAuth: true } },
  { path: "/stats", component: StatsView, meta: { requiresAuth: true } },
  { path: "/risk", component: RiskView, meta: { requiresAuth: true } },
  { path: "/admin", component: AdminView, meta: { requiresAuth: true, adminOnly: true } }
];
