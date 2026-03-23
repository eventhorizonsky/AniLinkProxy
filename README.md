# AniLink 弹弹代理服务（proxy-project）

本目录实现了一个轻量化（1核1G 优先）的弹弹 API 代理服务：

- 后端：Go + SQLite（无 Redis 依赖）
- 前端：Vue3 + Vite + Vuetify
- 代理接口：`comment/search/bangumi/shin/match/match-batch`
- 管理能力：注册登录、邮箱验证码、Turnstile、人机校验后查看密钥、重置密钥、统计、风控记录、超管封禁与配置

## 本地开发

### 1) 后端

```bash
cd proxy-project/backend
go mod tidy
go run .
```

或使用快捷脚本（可一键设置环境变量并启动）：

```powershell
powershell -ExecutionPolicy Bypass -File .\start-backend.ps1
```

### 2) 前端

```bash
cd proxy-project/frontend
npm install
npm run dev
```

前端开发端口默认 `5173`，已代理到后端 `8080`。

## 生产部署（Docker）

```bash
cd proxy-project
docker build -t anilink-dandan-proxy:latest .
docker run --rm -p 8080:8080 --env-file .env anilink-dandan-proxy:latest
```

说明：
- 镜像运行阶段直接启动编译后的二进制（`/srv/proxy-server`），不会在容器内执行 `go run`。
- `Dockerfile` 是纯运行时镜像，不负责编译；需要提前准备：
  - `build/proxy-server`
  - `build/frontend-dist`
- GitHub Actions 会自动生成以上产物后再打包镜像。
- `Dockerfile` 已显式声明运行时所需环境变量默认值，生产环境请务必通过 `.env` 覆盖敏感项。

## 关键环境变量

- 上游签名：
  - `UPSTREAM_DANDAN_APP_ID`
  - `UPSTREAM_DANDAN_APP_SECRET`
- 数据与鉴权：
  - `SQLITE_PATH`
  - `JWT_SECRET`
- 邮件验证码：
  - `SMTP_HOST` `SMTP_PORT`
  - `SMTP_USERNAME` `SMTP_PASSWORD`
  - `SMTP_FROM_ADDRESS`
- Turnstile：
  - `TURNSTILE_SITE_KEY`
  - `TURNSTILE_SECRET_KEY`
- 初始超管（首次启动生效）：
  - `INIT_ADMIN_EMAIL`
  - `INIT_ADMIN_PASSWORD`

## 资源节约设计

- 缓存：内存 TTL 缓存，覆盖 `comment/search/bangumi/shin`
- 限流：内存 Token Bucket，按 `AppId + endpoint`
- 并发控制：`match/match-batch` 按 AppId 单飞锁
- 持久化：SQLite（WAL），减少外部中间件
