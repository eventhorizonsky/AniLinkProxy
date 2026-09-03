<template>
  <div>
    <div :id="containerId" ref="container"></div>
  </div>
</template>

<script setup>
import { nextTick, onBeforeUnmount, onMounted, ref } from "vue";

const props = defineProps({
  // CaptchaLa 的 Application Key（公开，对应后台的 CAPTCHALA_SITE_KEY）。
  appKey: { type: String, required: true },
  // 业务场景标识（login / register / ...），与后端校验的 action 保持一致。
  action: { type: String, default: "default" }
});

const emit = defineEmits(["verified", "expired", "error", "ready"]);

// CaptchaLa 的 Web SDK 由 loader 脚本提供（核心 SDK 从 CDN 动态加载）。
const LOADER_URL = import.meta.env.VITE_CAPTCHALA_LOADER_URL || "https://cdn.captcha-cdn.net/captchala-loader.js";

const container = ref(null);
const containerId = `captchala-${Math.random().toString(36).slice(2, 8)}`;
let instance = null;
let destroyed = false;

function loadLoader() {
  return new Promise((resolve, reject) => {
    if (window.loadCaptchala) {
      resolve();
      return;
    }
    const existed = document.querySelector("script[data-captchala-loader='1']");
    if (existed) {
      existed.addEventListener("load", () => resolve(), { once: true });
      existed.addEventListener("error", () => reject(new Error("captchala loader load failed")), { once: true });
      return;
    }
    const script = document.createElement("script");
    script.src = LOADER_URL;
    script.async = true;
    script.defer = true;
    script.dataset.captchalaLoader = "1";
    script.onload = () => resolve();
    script.onerror = () => reject(new Error("captchala loader load failed"));
    document.head.appendChild(script);
  });
}

async function initWidget() {
  if (!props.appKey || !container.value) return;
  await new Promise((resolve, reject) => {
    window.loadCaptchala(
      () => resolve(),
      (err) => reject(err || new Error("captchala load failed"))
    );
  });
  if (destroyed || !container.value) return;
  const api = window.Captchala;
  if (!api) {
    emit("error", new Error("captchala SDK unavailable"));
    return;
  }
  instance = api.init({
    appKey: props.appKey,
    product: "embed",
    action: props.action,
    lang: "auto"
  });
  instance
    .appendTo(`#${containerId}`)
    .onSuccess((res) => {
      if (res && res.token) emit("verified", res.token);
      else emit("error", new Error("captchala returned no token"));
    })
    .onError((err) => emit("error", err?.message || err || new Error("captchala error")));
  emit("ready");
}

function reset() {
  instance?.reset?.();
}

defineExpose({ reset });

onMounted(async () => {
  try {
    await loadLoader();
    await nextTick();
    await initWidget();
  } catch (e) {
    emit("error", e);
  }
});

onBeforeUnmount(() => {
  destroyed = true;
  try {
    instance?.destroy?.();
  } catch {
    // ignore
  }
  instance = null;
});
</script>
