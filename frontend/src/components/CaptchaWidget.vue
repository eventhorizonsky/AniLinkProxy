<template>
  <div style="position: relative; min-height: 72px">
    <div
      v-if="loading"
      class="d-flex align-center justify-center"
      style="position: absolute; inset: 0; z-index: 2; gap: 10px; border: 1px dashed rgba(128,128,128,0.35); border-radius: 10px; background: rgba(128,128,128,0.08); backdrop-filter: blur(1px)"
    >
      <v-progress-circular indeterminate size="22" width="2.5" color="primary" />
      <span class="text-body-2 text-medium-emphasis">正在加载人机验证…</span>
    </div>
    <div>
      <template v-if="currentProvider === 'local'">
        <LocalCaptchaWidget
          ref="childRef"
          @verified="(t) => emit('verified', t)"
          @expired="() => emit('expired')"
          @error="onChildError"
          @ready="loading = false"
        />
      </template>
      <template v-else-if="currentProvider === 'captchala' && siteKeys.captchala">
        <CaptchaLaWidget
          ref="childRef"
          :app-key="siteKeys.captchala"
          :action="action"
          @verified="(t) => emit('verified', t)"
          @expired="() => emit('expired')"
          @error="onChildError"
          @ready="loading = false"
        />
      </template>
      <template v-else-if="currentProvider === 'turnstile' && siteKeys.turnstile">
        <TurnstileWidget
          ref="childRef"
          :site-key="siteKeys.turnstile"
          @verified="(t) => emit('verified', t)"
          @expired="() => emit('expired')"
          @error="onChildError"
          @ready="loading = false"
        />
      </template>
      <v-alert v-else-if="errorMsg" type="error" variant="tonal">
        {{ errorMsg }}
      </v-alert>
      <v-alert v-else type="warning" variant="tonal">
        人机验证尚未配置，请联系管理员设置 CAPTCHALA_*、TURNSTILE_SITE_KEY 或 CAPTCHA_PROVIDER=local。
      </v-alert>
    </div>
  </div>
</template>

<script setup>
import { computed, ref, watch } from "vue";
import CaptchaLaWidget from "./CaptchaLaWidget.vue";
import TurnstileWidget from "./TurnstileWidget.vue";
import LocalCaptchaWidget from "./LocalCaptchaWidget.vue";

const props = defineProps({
  provider: { type: String, default: "" },
  siteKeys: {
    type: Object,
    default: () => ({ captchala: "", turnstile: "" })
  },
  // CaptchaLa 业务场景标识，透传给 CaptchaLaWidget（login / register / ...）。
  action: { type: String, default: "default" }
});

const emit = defineEmits(["verified", "expired", "error", "provider-change"]);
const childRef = ref(null);
const loading = ref(false);
const errorMsg = ref("");

// 当前选中的提供方；由外部 provider 决定，渲染失败时可自动切到备用提供方。
const selected = ref(props.provider || "");

// 请求 provider 为 local 时直接渲染纯本地验证码；否则按配置的站点 key 降级逐步渲染 captchala/turnstile。
const currentProvider = computed(() => {
  if (selected.value === "local") return "local";
  if (selected.value === "captchala" && props.siteKeys.captchala) return "captchala";
  if (selected.value === "turnstile" && props.siteKeys.turnstile) return "turnstile";
  if (props.siteKeys.captchala) return "captchala";
  if (props.siteKeys.turnstile) return "turnstile";
  return "";
});

// 外部（父组件切换备用提供方）更新 provider 时，同步本地选中状态。
watch(
  () => props.provider,
  (p) => {
    selected.value = p;
  }
);

// 选中一个提供方后进入加载态；子组件 ready 时结束加载态。
watch(
  () => currentProvider.value,
  (p) => {
    loading.value = !!p;
  },
  { immediate: true }
);

// 某个提供方的组件加载/初始化失败时：若另一个提供方已配置则自动切换并通知父组件，
// 否则显示错误而不是留白，避免用户看到空白区域而不知发生了什么。
function onChildError(e) {
  loading.value = false;
  const cur = currentProvider.value;
  if (cur === "local") {
    // 本地验证码无备用提供方，直接提示错误。
    errorMsg.value = e?.message || "本地验证码加载失败，请刷新后重试";
    emit("error", e);
    return;
  }
  const other = cur === "captchala" ? "turnstile" : "captchala";
  const otherKey = other === "captchala" ? props.siteKeys.captchala : props.siteKeys.turnstile;
  if (cur && otherKey) {
    errorMsg.value = "";
    selected.value = other;
    emit("provider-change", other);
    emit("error", e);
    return;
  }
  errorMsg.value = e?.message || "人机验证组件加载失败，请刷新后重试";
  emit("error", e);
}

function reset() {
  errorMsg.value = "";
  childRef.value?.reset?.();
}

defineExpose({ reset });
</script>
