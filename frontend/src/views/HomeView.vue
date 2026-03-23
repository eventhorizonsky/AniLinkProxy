<template>
  <v-card>
    <v-card-title>首页</v-card-title>
    <v-card-text>
      <p>该服务兼容弹弹签名头：`X-AppId/X-Timestamp/X-Signature`。</p>
      <p>请在「我的密钥」查看 AppId，并通过邮箱二次验证查看或重置 Secret。</p>
      <p>为了 1核1G 服务器节省资源，系统默认采用 SQLite + 内存缓存/限流，不依赖 Redis。</p>
    </v-card-text>
  </v-card>
</template>
