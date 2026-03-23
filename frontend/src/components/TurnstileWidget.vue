<template>
  <div>
    <div ref="container"></div>
  </div>
</template>

<script setup>
import { nextTick, onBeforeUnmount, onMounted, ref, watch } from "vue";

const props = defineProps({
  siteKey: { type: String, required: true }
});

const emit = defineEmits(["verified", "expired", "error"]);
const container = ref(null);
let widgetId = null;

function loadScript() {
  return new Promise((resolve, reject) => {
    if (window.turnstile) {
      resolve();
      return;
    }
    const existed = document.querySelector("script[data-turnstile='1']");
    if (existed) {
      existed.addEventListener("load", () => resolve(), { once: true });
      existed.addEventListener("error", () => reject(new Error("turnstile script load failed")), { once: true });
      return;
    }
    const script = document.createElement("script");
    script.src = "https://challenges.cloudflare.com/turnstile/v0/api.js?render=explicit";
    script.async = true;
    script.defer = true;
    script.dataset.turnstile = "1";
    script.onload = () => resolve();
    script.onerror = () => reject(new Error("turnstile script load failed"));
    document.head.appendChild(script);
  });
}

function renderWidget() {
  if (!window.turnstile || !container.value || !props.siteKey) return;
  if (widgetId !== null) {
    try {
      window.turnstile.remove(widgetId);
    } catch {
      // ignore
    }
    widgetId = null;
  }
  widgetId = window.turnstile.render(container.value, {
    sitekey: props.siteKey,
    callback: (token) => emit("verified", token),
    "expired-callback": () => emit("expired"),
    "error-callback": () => emit("error")
  });
}

function reset() {
  if (window.turnstile && widgetId !== null) {
    window.turnstile.reset(widgetId);
  }
}

defineExpose({ reset });

onMounted(async () => {
  try {
    await loadScript();
    await nextTick();
    renderWidget();
  } catch (e) {
    emit("error", e);
  }
});

watch(
  () => props.siteKey,
  async () => {
    await nextTick();
    renderWidget();
  }
);

onBeforeUnmount(() => {
  if (window.turnstile && widgetId !== null) {
    try {
      window.turnstile.remove(widgetId);
    } catch {
      // ignore
    }
  }
});
</script>
