<template>
  <v-card :loading="loading">
    <v-card-title>我的调用统计</v-card-title>
    <v-card-text>
      <v-row align="end">
        <v-col cols="12" sm="6" md="4">
          <v-date-input
            v-model="fromDate"
            label="开始日期"
            density="comfortable"
            hide-details="auto"
            clearable
            first-day-of-week="1"
            name="anilink-stats-from"
            autocomplete="off"
            autocorrect="off"
            autocapitalize="off"
            spellcheck="false"
          />
        </v-col>
        <v-col cols="12" sm="6" md="4">
          <v-date-input
            v-model="toDate"
            label="结束日期"
            density="comfortable"
            hide-details="auto"
            clearable
            first-day-of-week="1"
            name="anilink-stats-to"
            autocomplete="off"
            autocorrect="off"
            autocapitalize="off"
            spellcheck="false"
          />
        </v-col>
        <v-col cols="12" md="4">
          <v-btn color="primary" block class="mb-1" :loading="loading" @click="load">查询</v-btn>
        </v-col>
      </v-row>
      <v-alert v-if="error" type="error" variant="tonal" class="mb-4">{{ error }}</v-alert>
      <div v-if="!loading && !error && rows.length === 0" class="text-medium-emphasis text-body-2 py-6 text-center">
        所选时间范围内暂无统计数据
      </div>
      <v-table v-else-if="rows.length > 0" class="stats-table">
        <thead>
          <tr>
            <th>接口</th>
            <th class="text-end">总请求</th>
            <th class="text-end">成功</th>
            <th class="text-end">鉴权失败</th>
            <th class="text-end">限流</th>
            <th class="text-end">上游失败</th>
            <th class="text-end">平均延迟(ms)</th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="item in rows" :key="item.endpoint">
            <td class="text-break">{{ item.endpoint }}</td>
            <td class="text-end">{{ item.total }}</td>
            <td class="text-end">{{ item.success }}</td>
            <td class="text-end">{{ item.authFail }}</td>
            <td class="text-end">{{ item.rateLimited }}</td>
            <td class="text-end">{{ item.upstreamFail }}</td>
            <td class="text-end">{{ Math.round(item.avgLatencyMs || 0) }}</td>
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

const fromDate = ref(null);
const toDate = ref(null);
const rows = ref([]);
const loading = ref(true);
const error = ref("");

function toYmdLocal(d) {
  if (d == null) return "";
  const date = d instanceof Date ? d : new Date(d);
  if (Number.isNaN(date.getTime())) return "";
  const y = date.getFullYear();
  const m = String(date.getMonth() + 1).padStart(2, "0");
  const day = String(date.getDate()).padStart(2, "0");
  return `${y}-${m}-${day}`;
}

async function load() {
  loading.value = true;
  error.value = "";
  try {
    const from = toYmdLocal(fromDate.value);
    const to = toYmdLocal(toDate.value);
    const res = await apiGet("/admin/api/stats/me", { from, to });
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
.stats-table {
  overflow-x: auto;
}
</style>
