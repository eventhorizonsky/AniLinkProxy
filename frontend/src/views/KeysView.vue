<template>
  <v-card class="mb-4" :loading="pageLoading">
    <v-card-title>我的密钥</v-card-title>
    <v-card-text>
      <p class="text-body-2 text-medium-emphasis mb-4">
        以下字段布局一致：左侧只读展示，右侧为复制。客户端请求路径需包含 <code>/api/v2/...</code>，与「API Base URL」拼接即为完整地址。
      </p>

      <div class="d-flex flex-column flex-sm-row align-stretch mb-3">
        <v-text-field
          :model-value="apiBaseUrlDisplay"
          label="API Base URL"
          readonly
          density="comfortable"
          hide-details="auto"
          class="flex-grow-1 mb-2 mb-sm-0 mr-sm-3"
        />
        <v-btn variant="tonal" min-width="100" class="align-self-stretch" @click="copyApiBase">复制</v-btn>
      </div>

      <div class="d-flex flex-column flex-sm-row align-stretch mb-3">
        <v-text-field
          :model-value="user?.appId || ''"
          label="AppId"
          readonly
          density="comfortable"
          hide-details="auto"
          class="flex-grow-1 mb-2 mb-sm-0 mr-sm-3"
        />
        <v-btn
          variant="tonal"
          min-width="100"
          class="align-self-stretch"
          :disabled="!user?.appId"
          @click="copyAppId"
        >
          复制
        </v-btn>
      </div>

      <div class="d-flex flex-column flex-sm-row align-stretch mb-2">
        <v-text-field
          :model-value="secretFieldValue"
          label="App Secret"
          readonly
          density="comfortable"
          hide-details="auto"
          class="flex-grow-1 mb-2 mb-sm-0 mr-sm-3"
          :type="secret ? (showSecret ? 'text' : 'password') : 'text'"
          :placeholder="secret ? '' : '请点击右侧「加载」从服务端获取'"
          autocomplete="off"
        >
          <template v-if="secret" #append-inner>
            <v-btn
              icon
              variant="text"
              size="small"
              tabindex="-1"
              :aria-label="showSecret ? '隐藏密钥' : '显示密钥'"
              @click.stop="showSecret = !showSecret"
            >
              <v-icon>{{ showSecret ? 'mdi-eye-off' : 'mdi-eye' }}</v-icon>
            </v-btn>
          </template>
        </v-text-field>
        <v-btn
          v-if="!secret"
          variant="tonal"
          min-width="100"
          class="align-self-stretch"
          :loading="revealLoading"
          @click="reveal"
        >
          加载
        </v-btn>
        <v-btn
          v-else
          variant="tonal"
          min-width="100"
          class="align-self-stretch"
          :loading="copySecretLoading"
          @click="copySecret"
        >
          复制
        </v-btn>
      </div>

      <v-divider class="my-4" />
      <div class="text-subtitle-2 mb-2">重置 Secret（先发送邮箱验证码）</div>
      <div class="d-flex flex-wrap mb-3">
        <v-btn :loading="sending" @click="sendCode">发送邮箱验证码</v-btn>
      </div>
      <v-text-field v-model="emailCode" label="邮箱验证码" autocomplete="one-time-code" />
      <div class="d-flex">
        <v-btn color="warning" :loading="resetLoading" @click="resetSecret">重置 Secret</v-btn>
      </div>
      <v-alert v-if="message" type="info" variant="tonal" class="mt-3">{{ message }}</v-alert>
      <v-alert v-if="error" type="error" variant="tonal" class="mt-3">{{ error }}</v-alert>
    </v-card-text>
  </v-card>
</template>

<script setup>
import { computed, onMounted, ref } from "vue";
import { apiGet, apiPost } from "../api";
import { showErrorSnackbar, showSuccessSnackbar } from "../snackbar";

const user = ref(null);
const pageLoading = ref(true);
const sending = ref(false);
const revealLoading = ref(false);
const resetLoading = ref(false);
const copySecretLoading = ref(false);
const emailCode = ref("");
const secret = ref("");
const showSecret = ref(false);
const message = ref("");
const error = ref("");

const originDisplay = computed(() => (typeof window !== "undefined" ? window.location.origin : ""));
const apiBaseUrlDisplay = computed(() => originDisplay.value || "");

const secretFieldValue = computed(() => secret.value || "");

async function loadMe() {
  pageLoading.value = true;
  error.value = "";
  try {
    const res = await apiGet("/admin/api/me");
    user.value = res.data;
  } catch (e) {
    error.value = e?.response?.data?.message || e.message || "加载失败";
    showErrorSnackbar(error.value);
  } finally {
    pageLoading.value = false;
  }
}

async function copyText(text, okMsg) {
  if (!text) return;
  try {
    await navigator.clipboard.writeText(text);
    showSuccessSnackbar(okMsg);
  } catch {
    showErrorSnackbar("复制失败，请手动选择复制");
  }
}

async function copyApiBase() {
  await copyText(apiBaseUrlDisplay.value, "API Base URL 已复制");
}

async function copyAppId() {
  const id = user.value?.appId;
  if (!id) return;
  await copyText(String(id), "AppId 已复制");
}

async function copySecret() {
  if (!secret.value) return;
  copySecretLoading.value = true;
  try {
    await copyText(secret.value, "Secret 已复制");
  } finally {
    copySecretLoading.value = false;
  }
}

async function sendCode() {
  sending.value = true;
  error.value = "";
  try {
    const res = await apiPost("/admin/api/secret/send-reset-code");
    if (res.code !== "OK") throw new Error(res.message || res.code);
    message.value = "验证码已发送到您的邮箱";
    showSuccessSnackbar("验证码已发送");
  } catch (e) {
    error.value = e?.response?.data?.message || e.message || "发送失败";
    showErrorSnackbar(error.value);
  } finally {
    sending.value = false;
  }
}

async function reveal() {
  error.value = "";
  revealLoading.value = true;
  try {
    const res = await apiPost("/admin/api/secret/reveal", {});
    if (res.code !== "OK") throw new Error(res.message || res.code);
    secret.value = res.data.appSecret;
    showSecret.value = false;
    showSuccessSnackbar("Secret 已加载");
  } catch (e) {
    error.value = e?.response?.data?.message || e.message || "加载失败";
    showErrorSnackbar(error.value);
  } finally {
    revealLoading.value = false;
  }
}

async function resetSecret() {
  error.value = "";
  resetLoading.value = true;
  try {
    const res = await apiPost("/admin/api/secret/reset", { emailCode: emailCode.value });
    if (res.code !== "OK") throw new Error(res.message || res.code);
    secret.value = res.data.appSecret;
    showSecret.value = false;
    message.value = "Secret 已重置，旧 Secret 立即失效。";
    showSuccessSnackbar("Secret 已重置");
  } catch (e) {
    error.value = e?.response?.data?.message || e.message || "重置失败";
    showErrorSnackbar(error.value);
  } finally {
    resetLoading.value = false;
  }
}

onMounted(loadMe);
</script>
