# SeaweedFS 的 115 冷存储接口

更新时间：2026-10-15。

这是独立的 SeaweedFS 适配。接口前缀是 `/seaweedfs/v1`，默认关闭。
现有 `/restic`、普通 `/d` 下载、Jellyfin 302、AutoFilm 和 115 登录逻辑沿用原有实现。

## 数据路径

SeaweedFS 将只读卷上传到此接口。网关校验整个文件的 SHA-256，调用现有
115 上传方法，并将上传返回的文件 ID、PickCode 等定位信息保存到 OpenList 数据库。
只有上传完成且本地回执提交成功才返回成功。

读取优先复用本地持久化 URL 缓存；缓存缺失或失效时才根据回执调用 115 下载地址接口，返回 302。
实际文件字节通过 Range 从下载地址获取。正常上传、读取、HEAD、列举对象和重启
均不调用远程目录列表接口。空间目录需要提前创建，并填写明确的目录 ID。

本接口保存收到的字节，不负责加密。对应 SeaweedFS 必须启用
`-filer.encryptVolumeData`；使用 S3 时还须启用 `-s3.encryptVolumeData`。
禁止将未加密的业务内容直接 PUT 到此接口。

## 配置

在 OpenList 的 `config.json` 增加以下顶层配置，替换示例 Token 和目录 ID：

```json
{
  "seaweedfs": {
    "enable": true,
    "max_object_mib": 64,
    "read_interval_ms": 5000,
    "cooldown_seconds": 1800,
    "recovery_token": "replace-with-a-separate-random-secret-at-least-32-characters",
    "spaces": [
      {
        "name": "notes",
        "token": "replace-with-a-random-space-secret",
        "mount_path": "/115",
        "parent_id": "REPLACE_WITH_NOTES_DIRECTORY_ID"
      },
      {
        "name": "photos",
        "token": "replace-with-a-different-random-space-secret",
        "mount_path": "/115",
        "parent_id": "REPLACE_WITH_PHOTOS_DIRECTORY_ID"
      }
    ]
  }
}
```

每个业务使用独立空间、Token 和云端目录。仅支持实现 `driver.TierProvider` 的存储驱动，
本次实现的是 `drivers/115`。MountPath 必须与已配置存储的挂载位置完全一致。
修改挂载、目录或删除后重新创建存储，不会自动重新绑定已有回执。

部署一个网关实例。并发控制在进程内，持久化冷却状态不能替代多个网关实例之间的分布式锁。
远程访问网关使用 HTTPS 或受保护的 NAS 网络；此 Token 是整个业务空间的管理凭据，
不能直接交给普通浏览器客户端。

## 风控规则

- 全网关上传队列并发为 1；调用已有 115 账户调度器，与现有业务共享账户请求及上传限制。
- 通过专用上下文标记，使 SeaweedFS 的不超过 64 MiB 上传使用普通 OSS 上传。
  Backrest 已有的 64 MiB 规则和普通上传的 10 MiB 规则保留。
- `read_interval_ms` 控制新下载链接的申请间隔，默认 5 秒，按 115 账户共享。缓存命中不消耗额度，有效 URL 可以重复使用并同时下载不同 pack。过快的新申请在本地返回 `429 Retry-After`，客户端等待后重试。
- URL 缓存保存在 OpenList 数据库，重启继续复用。115 未提供明确有效期时不设置猜测的 TTL；只有提供方明确返回有效期时才使用该时间。
- 下载出现 401/403/404/410 时，客户端报告失败 URL，使其失效，再排队申请新地址。相同旧 URL 的并发报告只处理一次，迟到的旧报告不会使新 URL 失效。
- 首次 403 按可能过期处理；新链接仍然返回 403 时进入持久化冷却，成功读取清除连续失败计数。405/429 及提供方明确阻断直接冷却。冷却期间新上传和新链接申请返回 `503 Retry-After`，其他仍有效的缓存 URL 可以继续使用。
- 每次读取最多刷新两次链接，等待受客户端超时约束；持续故障返回错误，避免自动重试持续请求 115。
- 115 驱动的账户请求限速器作用于共享客户端的全部 API 请求，包括申请链接、上传初始化、离线任务提交及目录操作。上传还使用已有上传队列。CDN 文件传输不占用链接申请额度。

新接口的持久化冷却只控制 SeaweedFS 的新请求，既有 Jellyfin 和 Restic 冷却逻辑保持原样；各入口继续共用原有 115 账户请求调度器。默认间隔是配置起点，实际可按账户负载调整。

## API 与一致性

所有请求需要 `Authorization: Bearer <space-token>`。
`key` 是上传内容 SHA-256 的 64 位小写十六进制字符串。

| API | 行为 |
| --- | --- |
| `PUT /{space}/objects/{key}` | 必须提供 Content-Length；校验后上传；201 为已提交，重复成功为 200 |
| `GET /{space}/objects/{key}` | 优先返回持久缓存的 302；只有新链接申请经过限频 |
| `HEAD /{space}/objects/{key}` | 只读本地回执，返回长度、ETag；不接触 115 |
| `GET /{space}/objects?after={key}&limit=100` | 本地按 key 分页列举，limit 最大 1000；不返回定位信息 |
| `DELETE /{space}/objects/{key}` | 本地标记删除，保留云端字节，不执行在线物理回收 |
| `POST /{space}/objects/{key}/failure` | JSON `{"status":410,"url":"实际失败的旧地址"}` 报告失效；200/206 报告成功并清除连续失败计数 |
| `GET /{space}/inventory?after={key}` | 分页导出本地完整回执，每页最多 1000 条，继续使用返回的 next |
| `POST /{space}/inventory` | 导入回执；额外需要 `X-SeaweedFS-Recovery-Token`；整个批次原子提交 |

上传状态为 `uploading → ready`，失败保留 `pending`。不完整对象不能读取。
远程文件名包含内容摘要和每次尝试的 UUID，成功回执不可覆盖。
上传响应丢失时，重复尝试可能留下云端孤立文件；这比覆盖唯一有效副本更适合当前一致性要求。
重试相同已提交内容只校验本地请求体，不再次调用 115。

本地回执提交失败绝不返回上传成功，因此 SeaweedFS 保留原始 `.dat`。
数据库需要可靠的持久化配置和存储。恢复时优先恢复整个 OpenList 数据库；回执导入用于
指定空间的离线迁移，要求目标 key 不存在或与现有回执完全相同；相同回执可重复导入，不会扫描云端验证对象。

## 备份与阶段范围

OpenList 回执、SeaweedFS `.idx/.vif`、Filer 元数据及其中的解密密钥必须属于同一个可恢复的备份点。
还需要业务数据库与配置。仅保留 115 中的 pack，无法还原文件名、附件关系和解密密钥。
导出回执会包含 115 定位信息，应纳入加密备份。

SeaweedFS fork 的 `weed vault` 模块提供原子业务提交、关系打包、自动封存、客户端解密和统一云端检查点。
本接口新增 `GET /:space/receipts/:key` 精确读取完整回执，供检查点保存对象定位信息；
它只查询本地数据库。回执恢复接受完全相同的重复导入，拒绝覆盖不同记录。
自动物理空间回收尚未实现。
Backrest 不参与本接口的运行协议或恢复协议。

## 本地模拟测试

```bash
go build -o /tmp/openlist-lab ./cmd/seaweedfs-lab
cd ../seaweedfs
go build -o /tmp/weed ./weed
python3 test/openlist/integration.py --weed /tmp/weed --gateway /tmp/openlist-lab
```

测试运行真正的 SeaweedFS 和此网关，使用独立 SQLite 数据库、模拟 115 上传及 HTTP Range 下载。
覆盖加密内容、三业务目录、并发读取合并、限频、上传回执丢失、卷索引提交失败、重启、
CDN 阻断及凭据不向 CDN 发送。模拟器只监听回环地址，不加载 NAS 或 115 真实凭据。
它替代的是 115 驱动边界，不宣称通过真实 115 集成验证。

GitHub 新增适配构建仅使用 linux/amd64，产物为测试用二进制，不发布生产镜像标签。
