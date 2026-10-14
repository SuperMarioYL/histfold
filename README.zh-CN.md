[English](./README.md) | **简体中文**

<div align="center">

<img src="https://readme-typing-svg.demolab.com?font=JetBrains+Mono&weight=700&size=26&pause=1600&color=4A7BD0&center=true&vCenter=true&width=780&lines=One+agent+task.+Forty+micro-commits.;Fold+it+into+one+reviewable+changeset." alt="One agent task. Forty micro-commits. Fold it into one reviewable changeset.">

# Histfold

**把 Agent 时代的 git 历史，折叠成可评审的任务变更集。**

你的编码 Agent 一天提交上百次，git 历史从此不再是一份可评审的记录。Histfold
把原始的提交流折叠成带任务归因的变更集（changeset），维护者可以把 Agent 做的
一件事当作一个整体来评审，也能随时展开回原始提交。

[![CI](https://img.shields.io/github/actions/workflow/status/SuperMarioYL/histfold/ci.yml?branch=main&label=CI)](https://github.com/SuperMarioYL/histfold/actions/workflows/ci.yml)
[![Release](https://img.shields.io/github/v/release/SuperMarioYL/histfold)](https://github.com/SuperMarioYL/histfold/releases)
[![Go](https://img.shields.io/badge/Go-1.24-00ADD8?logo=go&logoColor=white)](https://go.dev)
[![Platform](https://img.shields.io/badge/platform-macOS%20%7C%20Linux-blue)](#快速上手)
[![License](https://img.shields.io/badge/license-MIT-green)](./LICENSE)

免费开源 · 单一静态二进制 · 零配置 · 只读

</div>

## 为什么需要折叠

人类过去用一个深思熟虑的提交交付的一项任务，现在会以几十个微型提交的形式到达，
其间还穿插着人工修改，并且没有任何机器可读的任务归因。翻 `git log` 重建不出任何
东西：单个提交没有意义，它们所属的任务只存在于磁盘上某个会话日志里。

围绕提交消息构建的工具解决不了这个问题。changelog 生成器和 `git log` 过滤器按
消息文本分组，而 Agent 的微型提交会让文本聚类淹没在生成的填充内容里。会话回放
工具向运行 Agent 的本人解释一次会话，却从不把整个仓库的提交流折叠成任务级的
单元。而 squash 会改写你正想评审的那份记录本身。

Histfold 做的是回溯式折叠（retroactive fold）。它关联三种相互独立的信号——
Agent 会话日志、触碰的文件路径、提交消息里的 Agent trailer——从已经写好的历史
中重建任务单元，并以严格只读的方式呈现。提交本身绝不被改动；变更集只是覆盖在
提交之上的一层视图。

## 快速上手

```bash
go install github.com/SuperMarioYL/histfold@latest
cd your-repo
histfold fold
open histfold.html
```

`histfold fold` 读取 `git log`，自动发现 `~/.claude/projects/<repo-slug>/`
下的 Claude Code 会话日志，打印折叠摘要，并生成一个自包含的 `histfold.html`
——一条可折叠的时间线，每个变更集是一张收起的卡片，可以展开回它的原始提交。
没有配置文件，不需要任何设置，仓库本身也不会被修改。

环境要求：安装需要 Go 1.24+（或者直接从
[Releases](https://github.com/SuperMarioYL/histfold/releases) 下载预编译二进制），
PATH 里有 git 2.30+，支持 macOS 和 Linux。

没有会话日志的仓库照样能折叠：Agent trailer 和时间间隔聚类会接手，只是置信度
更低。想立刻看一次满配的折叠？跑一下[演示](#演示)。

## 演示

手边没有重度使用 Agent 的仓库？`demo/seed.sh` 可以生成一个合成的：212 个交错的
微型提交——八段 Claude 任务脉冲及其配套的会话日志（写入
`~/.claude/projects/`，Histfold 会自动发现它们）、一段只有 trailer 的 Codex
脉冲、若干人工跟进提交，还有一段留在未合并分支上的提交。

```bash
git clone https://github.com/SuperMarioYL/histfold
cd histfold
demo/seed.sh
cd histfold-demo
histfold fold
```

以下是实录运行的输出（完整、可复现的记录见
[docs/demo-results.json](docs/demo-results.json)）：

```text
212 commits → 9 changesets
22 files touched, +212/-0

INTENT                                       AGENT   MODEL              FILES  REVIEW   CONF  COMMITS
Add cursor pagination to the user list API   Claude  claude-sonnet-4-5  3      on-main  90%   28
Fix the timezone bug in report exports       Claude  claude-sonnet-4-5  2      on-main  90%   30
Add rate limiting middleware                 Claude  claude-opus-4-6    3      on-main  90%   26
Refactor config loading into one package     Claude  claude-sonnet-4-5  3      on-main  90%   22
Add a CSV export endpoint                    Claude  claude-haiku-4-5   2      on-main  90%   28
Fix the N+1 query on the dashboard           Claude  claude-opus-4-6    2      on-main  90%   18
Add request tracing to the API handlers      Claude  claude-sonnet-4-5  2      on-main  90%   20
cache: inline lookups                        Codex   codex-max          1      on-main  50%   12
Migrate handler tests to table-driven style  Claude  claude-sonnet-4-5  4      branch   70%   28

confidence: 90% session+paths · 70% session time · 50% trailers · 20% time gaps
```

同一次运行还可以脚本化使用：`histfold fold --json | head -n 5` 输出机器可读的
变更集；`histfold fold` 同时会写出 `histfold.html`，在任何浏览器里离线打开
——零外部请求，任意卡片都能展开回原始提交。仓库里附带一段录好的终端会话
[assets/demo.cast](assets/demo.cast)（asciicast v3 格式，用
`asciinema play assets/demo.cast` 播放）；它覆盖终端部分，时间线本身则在你的
浏览器里打开。

种子脚本是确定性的——内容与时间戳全部固定——所以这次折叠在任何机器上都能
原样复现。清理时删除仓库目录和 seeder 结束时打印的会话日志目录即可。

## 使用方法

```text
histfold fold            # 终端摘要 + 写出 histfold.html
histfold fold --json     # 变更集以 JSON 输出到 stdout
histfold commits         # 归一化的提交流，附 Agent 归因
histfold version
```

`histfold commits` 展示原始素材：哈希、时间戳、作者、触碰的文件，以及每条提交
消息中识别出的 Agent 归因（trailer、`Co-Authored-By` 或 generated-with 脚注）。

### 读懂折叠结果

每个变更集都带一个置信度——它所用归因证据中较弱一环的强度：

| 置信度 | 证据 |
|---|---|
| 90% | 提交落在匹配会话的活动窗口内，且其路径与会话触碰的文件重叠 |
| 70% | 提交落在窗口内，但路径无法进一步确认匹配 |
| 50% | 仅凭提交 trailer（Agent、`Co-Authored-By` 等）归因，没有匹配到会话 |
| 20% | 纯时间间隔聚类，没有任何 Agent 信号 |

意图（intent）取自匹配会话的第一条用户提示——任务自己的措辞——没有会话时
退回到聚类首个提交的标题。评审状态（`on-main`、`branch`、`unknown`）仅从本地
引用推导；全程不发起任何网络请求。

### 它读什么、写什么

读取：`git log --numstat`（通过系统 git 二进制），以及当前仓库对应的
`~/.claude/projects/<repo-slug>/*.jsonl` 会话日志。写入：当前目录下的
`histfold.html`，以及 `--json` 打到 stdout 的内容。仅此而已——没有 hook，
没有配置，不改 git，工作区原封不动。

## 工作原理

一个 Go 二进制，三个内部包，没有服务进程和守护进程：

```text
            histfold (single binary)
┌───────────┬──────────────┬────────────────┐
│ ingest    │ fold         │ render         │
│ system    │ pure         │ terminal table │
│ git exec  │ correlation  │ + embedded     │
│ + claude  │ functions,   │ timeline.html  │
│ jsonl +   │ no I/O       │ (stdlib tpl,   │
│ trailers  │              │ vanilla JS)    │
└───────────┴──────────────┴────────────────┘
```

ingest 层调用系统 git（不内置 VCS 库，对各种仓库格式都稳健），并适配 Claude
Code 会话日志。fold 引擎是对这些数据的纯关联函数——折叠策略只存在于这一处，
非常容易测试。render 层打印终端表格，并用内嵌模板渲染自包含的 HTML 时间线：
原生 JS 折叠/展开，零 CDN 请求，离线可用。

v0.1 刻意保持单人模式：你自己的仓库，你自己的会话日志。折叠只关联本机上能找到
的来源，目前也只适配了 Claude Code 的 JSONL 日志。

## 路线图

- Claude Code JSONL 之外的会话适配器（Codex、Cursor 日志）。
- 面向团队仓库的跨贡献者折叠——需要先解决会话日志共享的方案，所以这是路线图
  而非承诺。
- Windows 构建（v0.1 仅支持 macOS 和 Linux）。

## 许可证

[MIT](./LICENSE) © 2026 SuperMarioYL

<p align="center"><sub><a href="./LICENSE">MIT</a> © 2026 SuperMarioYL</sub></p>
