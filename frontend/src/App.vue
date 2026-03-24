<template>
  <v-app>
    <v-navigation-drawer
      v-if="isAuthed && mdAndDown"
      v-model="drawer"
      temporary
      location="start"
    >
      <v-list density="compact" nav>
        <v-list-item to="/" title="首页" @click="drawer = false" />
        <v-list-item to="/keys" title="我的密钥" @click="drawer = false" />
        <v-list-item to="/stats" title="我的统计" @click="drawer = false" />
        <v-list-item to="/risk" title="风控记录" @click="drawer = false" />
        <v-list-item v-if="role === 'admin'" to="/admin" title="超管" @click="drawer = false" />
        <v-divider class="my-2" />
        <v-list-item title="退出" @click="onLogoutClick" />
      </v-list>
    </v-navigation-drawer>

    <v-app-bar color="primary" density="comfortable" v-if="isAuthed">
      <v-app-bar-nav-icon class="d-md-none" @click="drawer = true" />
      <v-app-bar-title class="text-truncate pr-2">AniLink Dandan Proxy</v-app-bar-title>
      <div class="d-none d-md-flex align-center">
        <v-btn variant="text" to="/" :active="route.path === '/'">首页</v-btn>
        <v-btn variant="text" to="/keys" :active="route.path === '/keys'">我的密钥</v-btn>
        <v-btn variant="text" to="/stats" :active="route.path === '/stats'">我的统计</v-btn>
        <v-btn variant="text" to="/risk" :active="route.path === '/risk'">风控记录</v-btn>
        <v-btn v-if="role === 'admin'" variant="text" to="/admin" :active="route.path === '/admin'">超管</v-btn>
      </div>
      <v-spacer />
      <v-btn class="d-none d-md-flex" variant="text" @click="logout">退出</v-btn>
    </v-app-bar>
    <v-main>
      <v-container class="py-6 py-md-8">
        <!-- 不使用 transition mode=out-in：与部分子页面组合时易出现切换后空白，直至刷新 -->
        <router-view />
      </v-container>
    </v-main>

    <v-snackbar
      v-model="snackbar.show"
      :color="snackbar.color"
      :timeout="snackbar.timeout"
      location="top"
      multi-line
    >
      {{ snackbar.text }}
    </v-snackbar>
  </v-app>
</template>

<script setup>
import { ref } from "vue";
import { useRoute, useRouter } from "vue-router";
import { useDisplay } from "vuetify";
import { authState, clearAuthServer } from "./auth";
import { snackbar } from "./snackbar";

const router = useRouter();
const route = useRoute();
const { mdAndDown } = useDisplay();
const drawer = ref(false);
const isAuthed = authState.isAuthed;
const role = authState.role;

async function logout() {
  await clearAuthServer();
  drawer.value = false;
  router.push("/login");
}

async function onLogoutClick() {
  drawer.value = false;
  await logout();
}
</script>
