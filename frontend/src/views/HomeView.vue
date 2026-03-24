<template>
  <v-row>
    <v-col cols="12">
      <v-card>
        <v-card-title>欢迎使用</v-card-title>
        <v-card-text>
          <p class="text-body-1 mb-4">弹弹 Play API 代理控制台，可在此管理密钥、查看调用量与风控记录。</p>
          <div class="d-flex flex-wrap">
            <v-btn color="primary" class="mr-2 mb-2" to="/keys" prepend-icon="mdi-key-variant">我的密钥</v-btn>
            <v-btn variant="tonal" class="mr-2 mb-2" to="/stats" prepend-icon="mdi-chart-box-outline">调用统计</v-btn>
            <v-btn variant="tonal" class="mb-2" to="/risk" prepend-icon="mdi-shield-alert-outline">风控记录</v-btn>
          </div>
        </v-card-text>
      </v-card>
    </v-col>
  </v-row>
</template>
