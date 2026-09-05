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
    <span class="text-body-1" style="font-size: 1.15rem; letter-spacing: 1px; user-select: none">
      <template v-if="question">{{ question }}</template>
      <v-skeleton-loader v-else type="text" width="110" class="d-inline-block" />
    </span>
    <v-text-field
      v-model="answer"
      label="计算并输入答案"
      density="compact"
      hide-details="auto"
      class="flex-grow-1"
      maxlength="6"
      :disabled="loading || !question"
      @update:model-value="onAnswerChange"
    />
  </div>
</template>

<script setup>
import { onMounted, ref } from "vue";
import { apiGet } from "../api";

const emit = defineEmits(["verified", "expired", "error", "ready"]);

const question = ref("");
const challengeId = ref("");
const answer = ref("");
const loading = ref(false);

// 拉取一道新的本地验证码题目；作废旧题并清空父组件缓存的 token。
async function fetchChallenge() {
  loading.value = true;
  question.value = "";
  challengeId.value = "";
  answer.value = "";
  emit("expired");
  try {
    const res = await apiGet("/admin/api/auth/captcha/challenge");
    const data = res?.data || {};
    question.value = data.question || "";
    challengeId.value = data.challengeId || "";
    if (question.value && challengeId.value) {
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
