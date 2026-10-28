# 缓存行为与 S3 存储

[项目首页](../README_CN.md) · [English](cache-storage.md)

## 缓存容量与淘汰策略

缓存支持通过 `max_size_gb`（YAML）、`--cache-max-size`（CLI）或 `APT_PROXY_CACHE_MAX_SIZE`（环境变量）设置容量上限。当总缓存大小超过该限制时，代理会按 **最久未访问**（LRU）自动删除文件，直至总容量保持在限制以内。淘汰会在写入新条目时以及定期清理时执行。

- 设置为正整数（如 `20` 表示 20 GB）可启用容量限制与 LRU 淘汰。
- 设置 `--cache-max-size=0` 或 `APT_PROXY_CACHE_MAX_SIZE=0` 可关闭容量限制，不进行按容量淘汰；YAML 的 `max_size_gb: 0` 保留默认值。

同理，`--cache-ttl=0` / `APT_PROXY_CACHE_TTL=0` 关闭后端过期淘汰，
`--cache-cleanup-interval=0` / `APT_PROXY_CACHE_CLEANUP_INTERVAL=0` 关闭定期清理。
关闭后端 TTL 不改变 HTTP 新鲜度或上游缓存指令；仍为正值的容量限制可以在写入时执行淘汰。

进程重启后，在尚未有新访问之前，LRU 顺序按文件修改时间近似。

启用 `shared_cache.coordination_dir` 后，实例间共享磁盘缓存会关闭容量/TTL 淘汰、后台清理和启动暂存扫描；全局在线 purge 被拒绝。维护前必须停止所有共享实例，详见[共享仓库缓存](../docs/shared-repository-cache.md)。

## 缓存目录结构

磁盘后端在缓存目录下放四样东西。它们在第一次写入时出现，不是启动时就创建：

```
body/v1/<hashed-key>      响应体
header/v1/<hashed-key>    状态行、header，以及写入时间戳
staging/v1/               正在写入的条目，空闲时为空
stale-markers.json        失效状态
```

一个条目由 body 和 header 共同构成，两者作为一步发布：字节先写进 `staging/v1`，再 rename 到位，因此重复写入一个已缓存的文件，不会让并发的读者看到截断或空的条目。`staging/v1` 下的东西都不是缓存条目 —— **做容量统计、备份或 rsync 时请排除它。** 普通单实例模式会在下次启动时清理进程被杀留下的暂存文件；共享磁盘协调模式要求所有实例停止后离线清理。

普通模式下，`body/v1` 和 `header/v1` 计入 `max_size_gb`；暂存文件与失效标记不计入。Header 文件含私有完整性和代际元数据，其大小随响应字段变化。暂存文件由独立的 `resources.total_temp_mb` 预算约束。

S3 后端没有 `staging/` 前缀，对象写入直接落到最终 key —— 暂存依赖 rename，而 VFS 接口里没有。

## 缓存键与镜像选择

默认缓存条目以**改写后**的上游 URL 为基础，并区分 HTTP 方法和 Vary 选择字段，而不是只使用客户端请求的地址。改写发生在缓存
层之前，因此客户端请求 `/ubuntu/dists/noble/InRelease` 实际会被存在所选镜像的 URL
下 —— `http://mirrors.example.com/ubuntu/dists/noble/InRelease`。

由此带来一个实际影响：**换了镜像就等于换了一份新缓存。** 同一个客户端请求此时对应
不同的键，会被重新下载；而旧镜像下的条目仍然占着磁盘，直到 TTL 或 LRU 淘汰把它们
清掉。实测单个对象：

```
同一镜像，第二次请求   -> 上游只被访问了一次   （缓存命中）
切换镜像之后           -> 从新镜像重新拉取
```

镜像测速选优在启动时和显式刷新时执行。可选的[运行时镜像健康](../docs/runtime-mirror-health.md)
还能基于已观察到的仓库元数据执行有界故障切换。测速结果不会持久化，因此**重启会重选**，
`SIGHUP` 和 `POST /api/mirrors/refresh` 也会重新选优。刷新失败时保留完整的原有路由与缓存规则。

默认不会跨镜像共享条目，因为镜像内容未必相同。显式的 [shared_cache 映射](../docs/shared-repository-cache.md)
可让经过内容校验的 SHA256 by-hash 索引跨等价镜像复用，普通软件包和可变元数据不会因此获得共享身份。
启用跨进程协调后，非共享请求按进程隔离，重启会使这部分缓存重新预热。
希望默认模式下缓存能跨重启保持热度时，可**直接指定镜像**：

```bash
./apt-proxy --ubuntu=https://mirrors.tuna.tsinghua.edu.cn/ubuntu/ \
            --debian=https://mirrors.tuna.tsinghua.edu.cn/debian/
```

显式指定的镜像会被直接使用、不做测速，因此在整个部署周期内缓存键都是稳定的。

## S3 存储后端

除了写到本地目录，`apt-proxy` 也可以把所有缓存的 body / header 直接落到任何 **S3
兼容**的对象存储里。适合缓存需要跨容器/节点持久化、或本地磁盘空间吃紧的场景。
同一桶和前缀上的多实例并发写入不提供分布式代际发布与 miss 协调保证；
需使用独立前缀隔离实例。`shared_cache.coordination_dir` 仅支持 Unix 磁盘后端。

通过 `--storage-backend=s3`（或 `APT_PROXY_STORAGE_BACKEND=s3`、YAML 中的
`storage.backend: s3`）启用。`endpoint` 和 `bucket` 必填。需要认证时，请成对提供
`access_key` 与 `secret_key`；公开桶也可使用匿名访问。

**YAML 示例：**

```yaml
storage:
  backend: s3
  s3:
    endpoint: minio.example.com:9000     # host[:port]，不要带 scheme
    region: us-east-1                    # AWS S3 必填；多数 MinIO 服务忽略
    bucket: apt-proxy
    prefix: apt-proxy/                   # 可选，默认 "apt-proxy/"
    access_key: ${APT_PROXY_S3_ACCESS_KEY}
    secret_key: ${APT_PROXY_S3_SECRET_KEY}
    use_ssl: true
    use_path_style: false                # MinIO/Ceph 需为 true；AWS/R2/B2 用 false
    inline_max_mb: 32                    # ≤32 MiB 走内存缓冲；超过则落盘临时文件
                                         # 单次写入阈值；载荷副本还共同受 resources.total_temp_mb 约束。
    temp_dir: ""                         # 留空使用 os.TempDir()
```

**ENV 示例**（适合 Kubernetes / Docker）：

```bash
APT_PROXY_STORAGE_BACKEND=s3
APT_PROXY_S3_ENDPOINT=minio:9000
APT_PROXY_S3_BUCKET=apt-proxy
APT_PROXY_S3_ACCESS_KEY=...
APT_PROXY_S3_SECRET_KEY=...
APT_PROXY_S3_USE_SSL=false
APT_PROXY_S3_USE_PATH_STYLE=true
```

**CLI 示例：**

```bash
./apt-proxy \
  --storage-backend=s3 \
  --s3-endpoint=s3.us-west-2.amazonaws.com \
  --s3-region=us-west-2 \
  --s3-bucket=apt-proxy \
  --s3-access-key=$AWS_ACCESS_KEY_ID \
  --s3-secret-key=$AWS_SECRET_ACCESS_KEY
```

**兼容性矩阵：**

| 厂商 / 实现        | `endpoint`                                          | `use_ssl` | `use_path_style` | 备注                                   |
| ------------------ | ---------------------------------------------------- | --------- | ---------------- | -------------------------------------- |
| AWS S3             | `s3.<region>.amazonaws.com`                          | `true`    | `false`          | 必须显式设置 `region`                  |
| MinIO              | `minio.local:9000` / `<host>:9000`                   | 视部署    | `true`           | 必须 path-style                        |
| OtterIO            | `otterio:9000` / `<host>:9000`                       | 视部署    | `true`           | MinIO 的 Apache-2.0 衍生项目；配置相同 |
| Ceph RGW           | `rgw.example.com`                                    | 视部署    | `true`           | 必须 path-style                        |
| Cloudflare R2      | `<account>.r2.cloudflarestorage.com`                 | `true`    | `false`          | region 固定为 `auto`                   |
| Backblaze B2       | `s3.<region>.backblazeb2.com`                        | `true`    | `false`          | App Key 需对 bucket 有读写权限         |
| 阿里云 OSS         | `oss-cn-hangzhou.aliyuncs.com`                       | `true`    | `false`          | RAM 用户需 `oss:GetObject/PutObject`   |
| 腾讯云 COS         | `cos.ap-shanghai.myqcloud.com`                       | `true`    | `false`          | 使用 SecretId / SecretKey              |
| Garage / SeaweedFS | 视部署                                               | 视部署    | `true`           | 按 MinIO 风格配置                      |

**运维注意事项：**

- bucket 必须**预先存在**。`apt-proxy` 启动时会做一次 `BucketExists` 校验，配错
  立即报错退出，不会等到第一次 cache miss 才发现。
- `/healthz` 健康检查会感知存储后端：`s3` 模式做一次 HeadBucket，`disk` 模式做
  `os.Stat`。
- `s3vfs` 内置 8192 条元数据 LRU，用于吸收 `httpcache-kit` 频繁的 `Header()`
  访问。默认完整性校验仍须在命中前读取完整响应体，不能依赖元数据 LRU 跳过校验。
- 写入采用"智能"上传策略：≤ `inline_max_mb` 的对象在内存中累积后一次 PUT；超过
  阈值则落盘到临时文件再 `PutObject`。可根据典型包大小调整。
- S3 模式下 `cache.dir` / `--cachedir` / `APT_PROXY_CACHEDIR` 不作为持久缓存目录；
  大对象上传和未知长度下载仍需可写的本地暂存目录，TLS 证书也需要本地文件。当 `storage.backend=s3` 同时显式设置了非默认
  cache.dir 时，启动会输出一行警告，避免该特例被静默吞掉。

**资源容量规划（S3 后端）：**

S3 内联响应体与落盘副本都计入 `resources.total_temp_mb`（默认 4096 MiB）；
从内存切换到磁盘时，重叠的两份副本同时计费。`inline_max_mb`（默认 32 MiB）是
单次写入的阈值，不能把 `并发写入数 × inline_max_mb` 当作整个进程的 RSS 上限。
缓存异步发布可以在上游下载槽释放后继续持有预算。HTTP 缓冲、元数据和 Go 运行时
仍需要额外内存余量；可按典型包大小将内联阈值调到 4–8 MiB。

超过阈值后使用匿名暂存文件：文件名立即 unlink，描述符关闭或进程退出才回收空间。
`storage.s3.temp_dir` 留空时使用系统临时目录，必须可写；只读根文件系统应单独挂载可写卷。
磁盘检查同时考虑已保留暂存字节和 `resources.min_free_mb`（默认 128 MiB），失败时拒绝准入。
全局预算按实例计算，多个实例共享卷时必须分别分配额度。

在 Kubernetes 中，将暂存目录挂到 `emptyDir`，并显式设置 Pod 的内存与
`ephemeral-storage` 请求和上限。卷应能容纳所选暂存预算、磁盘保留空间及其他写入。
下面是应用配置示例，Pod 额度仍需根据实际负载验证：

```yaml
resources:
  max_object_mb: 1024
  total_temp_mb: 4096
  max_active_downloads: 32
  min_free_mb: 128
storage:
  backend: s3
  s3:
    inline_max_mb: 8
    temp_dir: /var/cache/apt-proxy/tmp
    # endpoint、bucket 和凭据见上方完整示例
```

S3 上传使用单次流式 PUT；`resources.max_object_mb` 不得超过 5120 MiB。
更多拒绝行为、超时与预算边界见[资源限制](../docs/resource-limits.md)。

完整可运行的示例（含 OtterIO + 自动建桶 + 接好的 apt-proxy）见
[`examples/s3-otterio/`](../examples/s3-otterio)。
