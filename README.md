# wasteoil —— 废弃油脂回收与生物柴油出口溯源平台

`wasteoil` 是一个纯 Go 实现的后端与命令行工具，覆盖餐饮废弃油脂的回收登记、
品质检测、转化计量、出口配额管理、溯源链构建、出口清单导出与海关申报。

项目不依赖任何第三方模块，只使用 Go 标准库。

## 业务背景

### 品质等级判定

依据游离脂肪酸（FFA）与水分杂质含量划分：

| 条件 | 等级 | 可否转化 |
| --- | --- | --- |
| FFA > 25% 或 水杂 > 8% | 不合格 | 否 |
| FFA ≤ 8% 且 水杂 ≤ 2% | 一级 | 是 |
| FFA ≤ 15% 且 水杂 ≤ 4% | 二级 | 是 |
| 其余 | 三级 | 是 |

### 转化计量口径

计量口径统一为**质量（千克）**。产出生物柴油以体积（升）计量，
转化批次同时记录产出密度（千克每升）。

```
得率 = 产出质量 / 投料质量
质量平衡: 投料质量 = 产出质量 + 副产甘油质量 + 工艺损耗质量
```

得率定义为产出生物柴油质量与投料废弃油脂质量之比，量纲为千克比千克。
行业正常得率区间为 **0.94 至 0.99**，落在区间之外的批次标记为
`"plausible": false`。质量平衡允许 0.5% 的相对偏差。

### 出口配额

出口配额是强约束额度：**任何时刻已预留总量都不得超过额度上限**，剩余额度不得为负。
并发预留下成功预留的次数最多为「额度上限 ÷ 单次预留量」，超出的请求应被拒绝并计入
`rejects`。

### 溯源链分叉

一批油脂在调配环节常被拆成多票出口货物，每票各自延续一条溯源链。
从同一前缀派生多条链时，**各条链必须持有独立存储**：每条链的末节点必须是
它自己追加的那个出口节点，链与链之间互不影响。

### 海关申报通道

调用方通过 context 控制单次申报的生命周期：**设定的超时到达或调用被取消时，
必须立即返回对应错误**，退出码 4。`gateway.Client` 另外保存一个基础 context，
用于后台保活与整体关闭。

### 清单写出的错误上报

写出过程中的任何失败都必须通过返回值上报给调用方，退出码 6；
**失败时不得留下可以正常回读的清单文件**。

## 目录结构

```
cmd/oilctl            命令行入口
internal/model        领域模型与哨兵错误
internal/collect      回收单位与回收登记台账
internal/convert      转化计量与质量平衡
internal/quota        出口配额预留与释放
internal/trace        出口溯源链构建与校验
internal/manifest     出口清单组装与写出
internal/gateway      海关单一窗口申报通道
internal/report       回收入库、转化与配额报表
internal/httpapi      HTTP 接口
internal/seed         内置样例数据
internal/cli          oilctl 命令实现
```

## 构建与测试

```bash
export GOTOOLCHAIN=local

go build ./...
go test ./...
go test -race ./...      # 出口配额并发预留需配合 -race 检查

make build           # 产出 bin/oilctl
make selfcheck       # 构建并运行内置自检
```

## 命令行用法

```bash
oilctl collector list
oilctl pickup list
oilctl pickup list --source restaurant
oilctl assay list

oilctl convert compute
oilctl convert compute --batch B-001

oilctl quota show
oilctl quota reserve --workers 64 --per-worker 5 --mass 10 --capacity 1000

oilctl trace build
oilctl manifest write --out ./tmp/manifest.json
oilctl manifest write --out ./tmp/manifest.json --inject-nonfinite   # 演练写出失败上报

oilctl customs submit --shipment S-001 --mass 9525.4 --timeout 3s
oilctl customs submit --timeout 120ms --customs-latency 5s           # 演练超时中止
oilctl customs probe --timeout 2s

oilctl report intake
oilctl report conversion

oilctl serve --addr 127.0.0.1:8080
oilctl selfcheck
```

### 退出码

| 退出码 | 含义 |
| --- | --- |
| 0 | 成功 |
| 1 | 用法错误或未归类的内部错误 |
| 2 | 参数非法 |
| 3 | 业务冲突（配额不足、品质不合格、资质停用） |
| 4 | 外部通道被取消或超时 |
| 5 | 资源不存在 |
| 6 | 数据一致性问题（质量平衡不闭合、溯源链断裂、清单写出失败） |

## HTTP 接口

```
GET  /healthz
GET  /api/collectors
GET  /api/collectors/{id}
GET  /api/pickups[?source=restaurant]
GET  /api/pickups/{id}
GET  /api/pickups/{id}/assay
GET  /api/batches
GET  /api/batches/{id}/yield
GET  /api/report/intake
GET  /api/report/conversion
GET  /api/quota
POST /api/quota/reserve
POST /api/customs/submit[?timeout_ms=5000]
GET  /api/customs/probe[?timeout_ms=2000]
```

错误响应统一为：

```json
{ "error": { "code": "quota_insufficient", "message": "..." } }
```

状态码约定：`404` 资源不存在，`409` 业务冲突，`422` 数据一致性问题，
`400` 参数非法，`502` 海关通道不可用，`503` 调用被取消或超时，
`500` 未归类的内部错误。

> 说明：HTTP 接口默认不带鉴权，仅面向内网或本地演练环境。若需暴露到公网，
> 必须在前置网关补充身份认证与访问控制。

## 容器运行

```bash
docker build -t wasteoil:local .
docker run --rm wasteoil:local selfcheck
docker run --rm -p 8080:8080 wasteoil:local serve --addr 0.0.0.0:8080
```

镜像基于 `golang:1.22` 构建、`distroless/static` 运行，同时支持
`linux/amd64` 与 `linux/arm64`：

```bash
docker build --platform linux/amd64 -t wasteoil:amd64 .
docker build --platform linux/arm64 -t wasteoil:arm64 .
```

## 数据来源

内置样例数据（`internal/seed`）包含 4 家回收单位、7 张回收单、7 条检测记录、
3 个转化批次与 3 票出口计划，年度出口配额 1000 吨。仅用于本地演练，
不代表真实经营数据。
