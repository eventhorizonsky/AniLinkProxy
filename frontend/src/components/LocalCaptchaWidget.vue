<template>
  <div class="d-flex align-center" style="gap: 8px; min-height: 48px">
    <v-btn
      icon
      size="small"
      variant="text"
      density="comfortable"
      title="换一题"
      :disabled="loading"
      @click="refresh"
    >
      <v-icon>mdi-refresh</v-icon>
    </v-btn>
    <span class="flex-grow-0">
      <img
        v-if="image"
        :src="image"
        alt="验证码"
        class="rounded"
        style="width: 160px; height: 54px; object-fit: contain; display: block; background: #eaeaea; cursor: pointer"
        title="点击刷新"
        @click="refresh"
      />
      <v-skeleton-loader v-else type="image" class="rounded" width="160" height="54" />
    </span>
    <v-text-field
      v-model="answer"
      label="输入图中数字"
      density="compact"
      hide-details="auto"
      class="flex-grow-1"
      maxlength="6"
      :disabled="loading || !image"
      @update:model-value="onAnswerChange"
    />
  </div>
</template>

<script setup>
import { onMounted, ref } from "vue";
import { apiGet } from "../api";

const emit = defineEmits(["verified", "expired", "error", "ready"]);

const image = ref("");
const challengeId = ref("");
const answer = ref("");
const loading = ref(false);

// 拉取一道新的本地图形验证码；作废旧题并清空父组件缓存的 token。
async function fetchChallenge() {
  loading.value = true;
  image.value = "";
  challengeId.value = "";
  answer.value = "";
  emit("expired");
  try {
    const res = await apiGet("/admin/api/auth/captcha/challenge");
    const data = res?.data || {};
    image.value = data.image || "";
    challengeId.value = data.challengeId || "";
    if (image.value && challengeId.value) {
      emit("ready");
    } else {
      emit("error", new Error("本地验证码加载失败"));
    }
  } catch (e) {
    emit("error", e?.response?.data?.message || e?.message || new Error("本地验证码加载失败"));
  } finally {
    loading.value = false;
  }
}

// 答案变化时以 "<challengeId>:<answer>" 作为 token 上报；答案为空则上报 expired 以清空父组件 token。
function onAnswerChange() {
  if (!challengeId.value) return;
  const v = String(answer.value || "").trim();
  if (v) emit("verified", `${challengeId.value}:${v}`);
  else emit("expired");
}

function refresh() {
  if (loading.value) return;
  fetchChallenge();
}

function reset() {
  // 清空输入并换取一道新题（fetchChallenge 内部会作废旧题并通知父组件清空 token）。
  fetchChallenge();
}

defineExpose({ reset });

onMounted(fetchChallenge);
</script>
