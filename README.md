# CFSM Agent 搭建

## 1. 准备监控面板

先部署 [CF-Server-Monitor](https://github.com/QiuXiaoye1112/CF-Server-Monitor)，登录后台添加主机。

## 2. 安装探针

在面板后台复制该主机的安装命令，到目标服务器执行。Linux 示例：

```sh
curl -fsSL https://raw.githubusercontent.com/QiuXiaoye1112/cfsm-agent/main/install.sh | sh -s -- install -id=SERVER_ID -secret=SECRET -url=https://YOUR_MONITOR_DOMAIN/update
```

替换 `SERVER_ID`、`SECRET` 和域名；推荐直接使用后台生成的完整命令。脚本从本仓库 Releases 下载匹配系统架构的程序。

Windows 使用后台生成的 PowerShell 命令，并在管理员 PowerShell 中运行。

## Docker

```sh
docker run -d --name cf-probe --restart=unless-stopped --network=host \
  -v cf-probe-data:/data \
  -e SERVER_ID=SERVER_ID -e SECRET=SECRET \
  -e WORKER_URL=https://YOUR_MONITOR_DOMAIN/update \
  ghcr.io/qiuxiaoye1112/cfsm-agent:latest
```

## 卸载

在面板后台复制卸载命令，使用原安装用户在目标服务器执行。Linux 示例：

```sh
curl -fsSL https://raw.githubusercontent.com/QiuXiaoye1112/cfsm-agent/main/install.sh | sh -s -- uninstall
```

## 从源码编译

安装 `go.mod` 指定版本或更新版本的 Go：

```sh
git clone https://github.com/QiuXiaoye1112/cfsm-agent.git
cd cfsm-agent
go test ./...
go build -trimpath -o cf-probe ./cmd/cf-probe
```
