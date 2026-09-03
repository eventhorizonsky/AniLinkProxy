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
      <template v-if="currentProvider === 'captchala' && siteKeys.captchala">
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
      <v-alert v-else type="warning" variant="tonal">
        人机验证尚未配置，请联系管理员设置 CAPTCHALA_* 或 TURNSTILE_SITE_KEY。
      </v-alert>
    </div>
  </div>
</template>

<script setup>
import { computed, ref, watch } from "vue";
import CaptchaLaWidget from "./CaptchaLaWidget.vue";
import TurnstileWidget from "./TurnstileWidget.vue";

const props = defineProps({
  provider: { type: String, default: "" },
  siteKeys: {
    type: Object,
    default: () => ({ captchala: "", turnstile: "" })
  },
  // CaptchaLa 业务场景标识，透传给 CaptchaLaWidget（login / register / ...）。
  action: { type: String, default: "default" }
});

const emit = defineEmits(["verified", "expired", "error"]);
const childRef = ref(null);
const loading = ref(false);

// 请求 provider 为 captchala 但未配置时，降级渲染 turnstile；反之亦然。
const currentProvider = computed(() => {
  if (props.provider === "captchala" && props.siteKeys.captchala) return "captchala";
  if (props.provider === "turnstile" && props.siteKeys.turnstile) return "turnstile";
  if (props.siteKeys.captchala) return "captchala";
  if (props.siteKeys.turnstile) return "turnstile";
  return "";
});

// 选中一个提供方后进入加载态；子组件 ready 时结束加载态。
watch(
  () => currentProvider.value,
  (p) => {
    loading.value = !!p;
  },
  { immediate: true }
);

function onChildError(e) {
  loading.value = false;
  emit("error", e);
}

function reset() {
  childRef.value?.reset?.();
}

defineExpose({ reset });
</script>
