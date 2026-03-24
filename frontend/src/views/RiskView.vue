<template>
  <v-card :loading="loading">
    <v-card-title class="d-flex align-center flex-wrap">
      <span>风控记录</span>
      <v-spacer />
      <v-btn size="small" variant="tonal" :loading="loading" @click="load">刷新</v-btn>
    </v-card-title>
    <v-card-text>
      <v-alert v-if="error" type="error" variant="tonal" class="mb-4">{{ error }}</v-alert>
      <div v-if="!loading && !error && rows.length === 0" class="text-medium-emphasis text-body-2 py-6 text-center">
        暂无风控记录
      </div>
      <v-table v-else-if="rows.length > 0" class="risk-table">
        <thead>
          <tr>
            <th>时间</th>
            <th>级别</th>
            <th>规则</th>
            <th>指标</th>
            <th>详情</th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="(item, idx) in rows" :key="idx">
            <td class="text-no-wrap">{{ item.createdAt }}</td>
            <td>{{ item.level }}</td>
            <td>{{ item.rule }}</td>
            <td>{{ item.metric }}</td>
            <td class="text-break">{{ item.detail }}</td>
          </tr>
        </tbody>
      </v-table>
    </v-card-text>
  </v-card>
</template>

<script setup>
import { onMounted, ref } from "vue";
import { apiGet } from "../api";
import { showErrorSnackbar } from "../snackbar";

const rows = ref([]);
const loading = ref(true);
const error = ref("");

async function load() {
  loading.value = true;
  error.value = "";
  try {
    const res = await apiGet("/admin/api/risk/me");
    rows.value = res.data || [];
  } catch (e) {
    const msg = e?.response?.data?.message || e.message || "加载失败";
    error.value = msg;
    rows.value = [];
    showErrorSnackbar(msg);
  } finally {
    loading.value = false;
  }
}

onMounted(load);
</script>

<style scoped>
.risk-table {
  overflow-x: auto;
}
</style>
