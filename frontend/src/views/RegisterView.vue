<template>
  <v-row justify="center">
    <v-col cols="12" md="6">
      <v-card>
        <v-card-title>注册（邮箱验证码 + Turnstile）</v-card-title>
        <v-card-text>
          <v-text-field v-model="email" label="邮箱" />
          <v-text-field v-model="password" label="密码（至少8位）" type="password" />

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

          <div class="d-flex">
            <v-text-field v-model="emailCode" label="邮箱验证码" class="mr-3" />
            <v-btn @click="sendCode" :loading="sending">发送邮箱验证码</v-btn>
          </div>
          <v-alert v-if="message" type="info" variant="tonal" class="mb-3">{{ message }}</v-alert>
          <v-alert v-if="error" type="error" variant="tonal" class="mb-3">{{ error }}</v-alert>
          <v-btn color="primary" block @click="register" :loading="loading">注册</v-btn>
          <v-btn class="mt-3" variant="text" block to="/login">返回登录</v-btn>
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

const router = useRouter();
const email = ref("");
const password = ref("");
const emailCode = ref("");
const message = ref("");
const error = ref("");
const sending = ref(false);
const loading = ref(false);
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

async function sendCode() {
  sending.value = true;
  error.value = "";
  try {
    if (!turnstileToken.value) throw new Error("请先完成 Turnstile 验证");
    const res = await apiPost("/admin/api/auth/email/send-register", {
      email: email.value,
      turnstileToken: turnstileToken.value
    });
    if (res.code !== "OK") throw new Error(res.message || res.code);
    message.value = "邮箱验证码已发送";
  } catch (e) {
    error.value = e?.response?.data?.message || e.message || "发送失败";
  } finally {
    sending.value = false;
    turnstileToken.value = "";
    turnstileRef.value?.reset?.();
  }
}

async function register() {
  loading.value = true;
  error.value = "";
  try {
    const res = await apiPost("/admin/api/auth/register", {
      email: email.value,
      emailCode: emailCode.value,
      password: password.value
    });
    if (res.code !== "OK") throw new Error(res.message || res.code);
    router.push("/login");
  } catch (e) {
    error.value = e?.response?.data?.message || e.message || "注册失败";
  } finally {
    loading.value = false;
  }
}

onMounted(loadTurnstileSiteKey);
</script>
