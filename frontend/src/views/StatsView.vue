<template>
  <v-card>
    <v-card-title>我的调用统计</v-card-title>
    <v-card-text>
      <v-row>
        <v-col cols="12" md="4"><v-text-field v-model="from" label="开始日期(YYYY-MM-DD)" /></v-col>
        <v-col cols="12" md="4"><v-text-field v-model="to" label="结束日期(YYYY-MM-DD)" /></v-col>
        <v-col cols="12" md="4"><v-btn color="primary" class="mt-2" @click="load">查询</v-btn></v-col>
      </v-row>
      <v-table>
        <thead>
          <tr>
            <th>接口</th>
            <th>总请求</th>
            <th>成功</th>
            <th>鉴权失败</th>
            <th>限流</th>
            <th>上游失败</th>
            <th>平均延迟(ms)</th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="item in rows" :key="item.endpoint">
            <td>{{ item.endpoint }}</td>
            <td>{{ item.total }}</td>
            <td>{{ item.success }}</td>
            <td>{{ item.authFail }}</td>
            <td>{{ item.rateLimited }}</td>
            <td>{{ item.upstreamFail }}</td>
            <td>{{ Math.round(item.avgLatencyMs || 0) }}</td>
          </tr>
        </tbody>
      </v-table>
    </v-card-text>
  </v-card>
</template>

<script setup>
import { ref } from "vue";
import { apiGet } from "../api";

const from = ref("");
const to = ref("");
const rows = ref([]);

async function load() {
  const res = await apiGet("/admin/api/stats/me", { from: from.value, to: to.value });
  rows.value = res.data || [];
}

load();
</script>
