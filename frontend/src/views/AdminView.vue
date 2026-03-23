<template>
  <v-card class="mb-4">
    <v-card-title>超管控制台</v-card-title>
    <v-card-text>
      <v-btn color="primary" @click="loadAll" class="mb-3">刷新数据</v-btn>
      <v-alert type="info" variant="tonal" class="mb-3">
        全局总请求: {{ global?.total || 0 }}，成功: {{ global?.success || 0 }}，鉴权失败: {{ global?.authFail || 0 }}，限流: {{ global?.rateLimited || 0 }}
      </v-alert>
      <v-expansion-panels>
        <v-expansion-panel title="运行参数">
          <v-expansion-panel-text>
            <v-text-field v-model.number="cfg.timestampToleranceSec" label="时间戳容忍秒数" />
            <v-text-field v-model.number="cfg.matchLockTimeoutSec" label="match锁超时秒数" />
            <v-text-field v-model.number="cfg.bodySizeLimitBytes" label="请求体大小限制" />
            <v-text-field v-model.number="cfg.batchMaxItems" label="批量match最大条数" />
            <v-switch v-model="cfg.timestampCheckEnabled" label="启用时间戳校验" />
            <v-switch v-model="cfg.autoBanEnabled" label="启用自动封禁" />
            <v-text-field v-model.number="cfg.autoBanMinutes" label="自动封禁时长(分钟)" />
            <v-btn color="primary" @click="saveCfg">保存配置</v-btn>
          </v-expansion-panel-text>
        </v-expansion-panel>
      </v-expansion-panels>
    </v-card-text>
  </v-card>

  <v-card>
    <v-card-title>账号管理</v-card-title>
    <v-card-text>
      <v-table>
        <thead>
          <tr>
            <th>ID</th>
            <th>邮箱</th>
            <th>AppId</th>
            <th>角色</th>
            <th>状态</th>
            <th>封禁到期</th>
            <th>操作</th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="u in users" :key="u.id">
            <td>{{ u.id }}</td>
            <td>{{ u.email }}</td>
            <td>{{ u.appId }}</td>
            <td>{{ u.role }}</td>
            <td>{{ u.status }}</td>
            <td>{{ u.banUntil }}</td>
            <td>
              <v-btn size="small" color="error" class="mr-2" @click="ban(u.id)">封禁</v-btn>
              <v-btn size="small" color="success" @click="unban(u.id)">解封</v-btn>
            </td>
          </tr>
        </tbody>
      </v-table>
    </v-card-text>
  </v-card>
</template>

<script setup>
import { ref } from "vue";
import { apiGet, apiPost, apiPut } from "../api";

const users = ref([]);
const global = ref({});
const cfg = ref({});

async function loadAll() {
  const [u, g, c] = await Promise.all([
    apiGet("/admin/api/admin/users"),
    apiGet("/admin/api/admin/stats/global"),
    apiGet("/admin/api/admin/config")
  ]);
  users.value = u.data || [];
  global.value = g.data || {};
  cfg.value = c.data || {};
}

async function ban(id) {
  await apiPost(`/admin/api/admin/users/${id}/ban`, { reason: "manual ban", minutes: 1440 });
  await loadAll();
}
async function unban(id) {
  await apiPost(`/admin/api/admin/users/${id}/unban`, {});
  await loadAll();
}
async function saveCfg() {
  await apiPut("/admin/api/admin/config", cfg.value);
  await loadAll();
}

loadAll();
</script>
