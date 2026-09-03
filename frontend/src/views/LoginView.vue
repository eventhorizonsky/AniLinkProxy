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
                <CaptchaWidget
                  ref="captchaRef"
                  :provider="captchaProvider"
                  :site-keys="captchaSiteKeys"
                  action="login"
                  @verified="onCaptchaVerified"
                  @expired="onCaptchaExpired"
                  @error="onCaptchaError"
                />
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
import CaptchaWidget from "../components/CaptchaWidget.vue";
import { setAuth } from "../auth";
import { showSuccessSnackbar } from "../snackbar";

const email = ref("");
const password = ref("");
const loading = ref(false);
const siteKeyLoading = ref(true);
const error = ref("");
const router = useRouter();
const captchaProvider = ref("");
const captchaSiteKeys = ref({ captchala: "", turnstile: "" });
const captchaToken = ref("");
const captchaRef = ref(null);
const formRef = ref(null);

const rules = {
  required: (v) => (v != null && String(v).trim() !== "") || "请填写此项"
};

function onCaptchaVerified(token) {
  captchaToken.value = token;
}

function onCaptchaExpired() {
  captchaToken.value = "";
}

function onCaptchaError() {
  captchaToken.value = "";
  error.value = "人机校验失败，请重试。";
}

async function loadCaptchaConfig() {
  siteKeyLoading.value = true;
  error.value = "";
  try {
    const res = await apiGet("/admin/api/auth/captcha/config");
    captchaProvider.value = res.data?.provider || "";
    captchaSiteKeys.value = res.data?.providers || { captchala: "", turnstile: "" };
  } catch (e) {
    error.value = e?.response?.data?.message || e.message || "加载验证组件失败";
  } finally {
    siteKeyLoading.value = false;
  }
}

function switchToFallbackProvider() {
  const next = captchaProvider.value === "captchala" ? "turnstile" : "captchala";
  if (!captchaSiteKeys.value?.[next]) {
    // 没有可用的备用提供方，只能停留在当前提供方重试。
    error.value = "人机验证服务暂不可用，请稍后重试";
    return false;
  }
  captchaProvider.value = next;
  captchaToken.value = "";
  captchaRef.value?.reset?.();
  return true;
}

async function submit() {
  const { valid } = await formRef.value?.validate?.();
  if (valid === false) return;

  loading.value = true;
  error.value = "";
  try {
    if (!captchaToken.value) throw new Error("请先完成人机验证");
    const res = await apiPost("/admin/api/auth/login", {
      email: email.value,
      password: password.value,
      captchaToken: captchaToken.value,
      captchaProvider: captchaProvider.value,
      captchaAction: "login"
    });
    if (res.code !== "OK") throw new Error(res.message || res.code);
    setAuth(res.data);
    showSuccessSnackbar("登录成功");
    router.push("/");
  } catch (e) {
    if (e?.response?.data?.code === "CAPTCHA_FALLBACK" && switchToFallbackProvider()) {
      error.value = "当前人机验证服务暂不可用，请用备用验证重新完成验证";
      return;
    }
    if (e?.response?.data?.code === "CAPTCHA_INVALID") {
      captchaRef.value?.reset?.();
    }
    error.value = e?.response?.data?.message || e.message || "登录失败";
  } finally {
    loading.value = false;
    captchaToken.value = "";
    // 每次提交后重置验证码（token 单次有效），失败/成功都让用户重新验证。
    captchaRef.value?.reset?.();
  }
}

onMounted(loadCaptchaConfig);
</script>
