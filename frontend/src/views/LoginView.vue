<template>
  <v-row justify="center">
    <v-col cols="12" sm="10" md="5">
      <v-card :loading="siteKeyLoading">
        <v-card-title>登录</v-card-title>
        <v-card-text>
          <v-form ref="formRef" @submit.prevent="submit">
            <v-text-field
              v-model="email"
              label="账号"
              type="text"
              autocomplete="username"
              :disabled="loading"
              :rules="[rules.required]"
            />
            <v-text-field
              v-model="password"
              label="密码"
              type="password"
              autocomplete="current-password"
              :disabled="loading"
              :rules="[rules.required]"
            />
            <div class="mb-3">
              <v-skeleton-loader v-if="siteKeyLoading" type="image" class="rounded" height="65" />
              <template v-else>
                <TurnstileWidget
                  v-if="turnstileSiteKey"
                  ref="turnstileRef"
                  :site-key="turnstileSiteKey"
                  @verified="onTurnstileVerified"
                  @expired="onTurnstileExpired"
                  @error="onTurnstileError"
                />
                <v-alert v-else type="warning" variant="tonal">Turnstile 未配置，请联系管理员设置 TURNSTILE_SITE_KEY。</v-alert>
              </template>
            </div>
            <v-alert v-if="error" type="error" variant="tonal" class="mb-3">{{ error }}</v-alert>
            <v-btn color="primary" block type="submit" :loading="loading" :disabled="siteKeyLoading">登录</v-btn>
          </v-form>
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
import { showSuccessSnackbar } from "../snackbar";

const email = ref("");
const password = ref("");
const loading = ref(false);
const siteKeyLoading = ref(true);
const error = ref("");
const router = useRouter();
const turnstileSiteKey = ref("");
const turnstileToken = ref("");
const turnstileRef = ref(null);
const formRef = ref(null);

const rules = {
  required: (v) => (v != null && String(v).trim() !== "") || "请填写此项"
};

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
  siteKeyLoading.value = true;
  error.value = "";
  try {
    const res = await apiGet("/admin/api/auth/turnstile/site-key");
    turnstileSiteKey.value = res.data?.siteKey || "";
  } catch (e) {
    error.value = e?.response?.data?.message || e.message || "加载验证组件失败";
  } finally {
    siteKeyLoading.value = false;
  }
}

async function submit() {
  const { valid } = await formRef.value?.validate?.();
  if (valid === false) return;

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
    showSuccessSnackbar("登录成功");
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
