<template>
  <v-row justify="center">
    <v-col cols="12" md="5">
      <v-card>
        <v-card-title>登录</v-card-title>
        <v-card-text>
          <v-text-field v-model="email" label="邮箱" />
          <v-text-field v-model="password" label="密码" type="password" />
          <div class="mb-3">
            <TurnstileWidget
              v-if="turnstileSiteKey"
              ref="turnstileRef"
              :site-key="turnstileSiteKey"
              @verified="onTurnstileVerified"
              @expired="onTurnstileExpired"
              @error="onTurnstileError"
            />
            <v-alert v-else type="warning" variant="tonal">Turnstile 未配置，请联系管理员设置 TURNSTILE_SITE_KEY。</v-alert>
          </div>
          <v-alert v-if="error" type="error" variant="tonal" class="mb-3">{{ error }}</v-alert>
          <v-btn color="primary" block @click="submit" :loading="loading">登录</v-btn>
          <v-btn class="mt-3" variant="text" block to="/register">去注册</v-btn>
        </v-card-text>
      </v-card>
    </v-col>
  </v-row>
</template>

<script setup>
import { onMounted, ref } from "vue";
import { useRouter } from "vue-router";
import { apiGet, apiPost } from "../api";
import TurnstileWidget from "../components/TurnstileWidget.vue";
import { setAuth } from "../auth";

const email = ref("");
const password = ref("");
const loading = ref(false);
const error = ref("");
const router = useRouter();
const turnstileSiteKey = ref("");
const turnstileToken = ref("");
const turnstileRef = ref(null);

function onTurnstileVerified(token) {
  turnstileToken.value = token;
}

function onTurnstileExpired() {
  turnstileToken.value = "";
}

function onTurnstileError() {
  turnstileToken.value = "";
  error.value = "Turnstile 校验失败，请重试。";
}

async function loadTurnstileSiteKey() {
  const res = await apiGet("/admin/api/auth/turnstile/site-key");
  turnstileSiteKey.value = res.data?.siteKey || "";
}

async function submit() {
  loading.value = true;
  error.value = "";
  try {
    if (!turnstileToken.value) throw new Error("请先完成 Turnstile 验证");
    const res = await apiPost("/admin/api/auth/login", {
      email: email.value,
      password: password.value,
      turnstileToken: turnstileToken.value
    });
    if (res.code !== "OK") throw new Error(res.message || res.code);
    setAuth(res.data);
    router.push("/");
  } catch (e) {
    error.value = e?.response?.data?.message || e.message || "登录失败";
  } finally {
    loading.value = false;
    turnstileToken.value = "";
    turnstileRef.value?.reset?.();
  }
}

onMounted(loadTurnstileSiteKey);
</script>
