# CFSM Agent (`cf-probe`)

配套 [CF-Server-Monitor](https://github.com/QiuXiaoye1112/CF-Server-Monitor)（支持 Cloudflare Workers 版与 VPS 独立自托管版）使用的 Go 探针客户端，提供系统资源采集、月流量持久化统计、WSS/HTTP 双通道上报以及高精度三网与自定义节点 Ping/丢包测量。

---

## Ping 与丢包检测机制（`v1.0.21+`）

- **10 秒全并发探测**：电信（CT）、联通（CU）、移动（CM）、BGP（BD）及 4 个自定义节点（Node 1~4）同时并发探测，启动即测，循环按 1 秒 tick 严格锚定 10 秒节奏，1 分钟滑动窗口固定 6 个样本。
- **3 秒超时 + TCP 重传假延迟拦截**：
  - 单次探测超时统一为 `3000ms`。
  - 当探测延迟 `> 1000ms` 时自动复测：在 `tcp` 模式下若复测延迟回落超过 `800ms`（典型的首包 SYN 丢失触发内核 1 秒 RTO 重传），直接判定该次为**丢包**（不再误报成 0% 丢包的高延迟）；连续复测 3 次仍持续高延迟同样判定为异常丢包。
- **两位小数精准丢包率**：1 分钟窗口内按 `cutoff` 过滤过期样本，使用浮点四舍五入输出两位小数丢包率（如 6 次丢 1 次为 `16.67%`、丢 2 次为 `33.33%`），消除整数截断误差。
- **支持 `tcp` 与 `icmp` 双模式**：默认 `tcp`（无需特殊权限）；如需纯 ICMP Echo 探测，可指定 `-ping_mode=icmp` 或在面板后台统一下发（Linux systemd 默认有权限，Docker 运行需额外加 `--cap-add=NET_RAW`）。

---

## 1. 准备监控面板

先部署 [CF-Server-Monitor](https://github.com/QiuXiaoye1112/CF-Server-Monitor)（Cloudflare Workers 版或 VPS 独立版均可），登录后台 `/admin` 添加主机。

---

## 2. 安装探针

推荐直接复制面板后台为该主机生成的完整安装命令并在目标服务器执行。Linux 手动安装示例：

```sh
curl -fsSL https://raw.githubusercontent.com/QiuXiaoye1112/cfsm-agent/main/install.sh | sh -s -- install \
  -id=SERVER_ID \
  -secret=SECRET \
  -url=https://YOUR_MONITOR_DOMAIN/update \
  -auto_update=1
```

脚本会自动识别系统与 CPU 架构，并从本仓库 [GitHub Releases](https://github.com/QiuXiaoye1112/cfsm-agent/releases) 下载对应的预编译二进制并注册为系统服务（Linux `systemd` / macOS `launchd`）。

Windows 请使用后台生成的 PowerShell 命令，在管理员 PowerShell 中运行。

### 常用安装选项

| 参数 | 默认值 | 说明 |
|---|---|---|
| `-id=SERVER_ID` | 必填 | 面板后台生成的服务器 ID |
| `-secret=SECRET` | 必填 | 服务端 `API_SECRET` |
| `-url=WORKER_URL` | 必填 | 上报接口地址，如 `https://monitor.example.com/update` |
| `-interval=N` | `60` | 历史落盘上报间隔（秒） |
| `-collect_interval=N` | `0` | 本地采样缓冲间隔（秒，`0` 表示关闭） |
| `-ct/-cu/-cm/-bd=HOST` | 空 | 自定义三网/BGP 测试目标，支持 `host` 或 `host:port` |
| `-interface=IFACES` | 自动汇总 | 指定统计流量的网卡，多个用英文逗号分隔 |
| `-reset_day=N` | `1` | 月流量重置日（`1-31`，`0` 表示不重置） |
| `-connection_mode=MODE` | `auto` | 连接模式：`auto`（优先 WSS，失败自动回退 HTTP）或 `http` |
| `-ping_mode=MODE` | `tcp` | Ping 模式：`tcp` 或 `icmp` |
| `-auto_update=0\|1` | `0` | 是否启用后台自动更新（见下方说明） |
| `-rx_correction=N` | 空 | 本月下行流量校正（GB） |
| `-tx_correction=N` | 空 | 本月上行流量校正（GB） |
| `-debug=0\|1` | `0` | 是否开启调试日志 |

---

## 3. 自动更新机制（`-auto_update=1`）

- 出于安全限制，自动更新开关只能由本地安装参数/配置文件（`/etc/config/cf-probe/config.conf` 中的 `AUTO_UPDATE="1"`）控制，服务端无法远程强行开启。
- 开启后，探针会在**启动时**以及之后**每 6 小时**检查一次 `QiuXiaoye1112/cfsm-agent` 的最新正式 Release：
  1. 发现更高版本且对应架构的二进制和 `checksums.txt` 已就绪；
  2. 下载并校验 **SHA-256 哈希**；
  3. 通过 `systemd-run` 延迟 1 分钟自动执行无感升级，完整保留现有配置与月流量记录（`traffic.dat`）并自动重启服务。

---

## 4. 常用运维命令（Linux systemd）

```sh
# 查看当前探针版本
/usr/local/bin/cf-probe -v

# 查看服务状态与实时日志
systemctl status cf-probe
journalctl -u cf-probe -f

# 重启或停止服务
systemctl restart cf-probe
systemctl stop cf-probe

# 查看当前配置
cat /etc/config/cf-probe/config.conf
```

---

## 5. Docker 运行

```sh
docker run -d --name cf-probe --restart=unless-stopped --network=host \
  -v cf-probe-data:/data \
  -e SERVER_ID=SERVER_ID \
  -e SECRET=SECRET \
  -e WORKER_URL=https://YOUR_MONITOR_DOMAIN/update \
  ghcr.io/qiuxiaoye1112/cfsm-agent:latest
```

> 若在容器中启用 `icmp` 探测模式，请额外添加 `--cap-add=NET_RAW`。

---

## 6. 卸载

在面板后台复制卸载命令，或直接执行：

```sh
curl -fsSL https://raw.githubusercontent.com/QiuXiaoye1112/cfsm-agent/main/install.sh | sh -s -- uninstall
```

---

## 7. 从源码编译与测试

需要安装 `go.mod` 指定版本或更新版本的 Go：

```sh
git clone https://github.com/QiuXiaoye1112/cfsm-agent.git
cd cfsm-agent
go test ./...
go build -trimpath -o cf-probe ./cmd/cf-probe
```

发布新版 Release（触发 GitHub Actions 自动编译全平台 16 个二进制 + `checksums.txt` + 多架构 Docker 镜像）：

```sh
gh release create v1.0.22 --repo QiuXiaoye1112/cfsm-agent --target main --title "v1.0.22" --generate-notes
```
