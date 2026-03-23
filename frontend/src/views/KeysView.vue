<template>
  <v-card class="mb-4">
    <v-card-title>我的密钥</v-card-title>
    <v-card-text>
      <v-text-field :model-value="user?.appId || '-'" label="AppId" readonly />
      <div class="d-flex">
        <v-btn color="primary" class="mr-3" @click="reveal">查看 Secret</v-btn>
      </div>

      <v-divider class="my-4" />
      <div class="text-subtitle-2 mb-2">重置 Secret（先发送邮箱验证码）</div>
      <div class="d-flex mb-3">
        <v-btn @click="sendCode" :loading="sending">发送邮箱验证码</v-btn>
      </div>
      <v-text-field v-model="emailCode" label="邮箱验证码" />
      <div class="d-flex">
        <v-btn color="warning" @click="resetSecret">重置 Secret</v-btn>
      </div>
      <v-alert v-if="secret" type="success" variant="tonal" class="mt-3">当前 Secret: {{ secret }}</v-alert>
      <v-alert v-if="message" type="info" variant="tonal" class="mt-3">{{ message }}</v-alert>
      <v-alert v-if="error" type="error" variant="tonal" class="mt-3">{{ error }}</v-alert>
    </v-card-text>
  </v-card>
</template>

<script setup>
import { onMounted, ref } from "vue";
import { apiGet, apiPost } from "../api";

const user = ref(null);
const sending = ref(false);
const emailCode = ref("");
const secret = ref("");
const message = ref("");
const error = ref("");

async function loadMe() {
  const res = await apiGet("/admin/api/me");
  user.value = res.data;
}

async function sendCode() {
  sending.value = true;
  error.value = "";
  try {
    const res = await apiPost("/admin/api/secret/send-reset-code");
    if (res.code !== "OK") throw new Error(res.message || res.code);
    message.value = "验证码已发送";
  } catch (e) {
    error.value = e?.response?.data?.message || e.message || "发送失败";
  } finally {
    sending.value = false;
  }
}

async function reveal() {
  error.value = "";
  try {
    const res = await apiPost("/admin/api/secret/reveal", {});
    if (res.code !== "OK") throw new Error(res.message || res.code);
    secret.value = res.data.appSecret;
  } catch (e) {
    error.value = e?.response?.data?.message || e.message || "查看失败";
  }
}

async function resetSecret() {
  error.value = "";
  try {
    const res = await apiPost("/admin/api/secret/reset", { emailCode: emailCode.value });
    if (res.code !== "OK") throw new Error(res.message || res.code);
    secret.value = res.data.appSecret;
    message.value = "Secret 已重置，旧 Secret 立即失效。";
  } catch (e) {
    error.value = e?.response?.data?.message || e.message || "重置失败";
  }
}

onMounted(async () => {
  await loadMe();
});
</script>
