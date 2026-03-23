<template>
  <v-card>
    <v-card-title>风控记录</v-card-title>
    <v-card-text>
      <v-table>
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
            <td>{{ item.createdAt }}</td>
            <td>{{ item.level }}</td>
            <td>{{ item.rule }}</td>
            <td>{{ item.metric }}</td>
            <td>{{ item.detail }}</td>
          </tr>
        </tbody>
      </v-table>
    </v-card-text>
  </v-card>
</template>

<script setup>
import { ref } from "vue";
import { apiGet } from "../api";

const rows = ref([]);
async function load() {
  const res = await apiGet("/admin/api/risk/me");
  rows.value = res.data || [];
}
load();
</script>
