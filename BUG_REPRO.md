# Bug Reproduction

## 包的性质

当前 test_model_fix 保存的是被测模型修复后的结果源码，不是初始含 Bug 源码。要复现原始缺陷，必须检出下面固定的 parent SHA；不要在当前修复结果源码上期待重新出现修复前失败。生成系统使用的可信验证补丁和完整验证日志仅在本地留存，不提交到结果分支。

## 问题现象

三票出口货物的溯源链，末节点全都指向同一票货，链校验直接报断裂。

```
$ ./oilctl trace build
{
  "chains": 3,
  "describe": [
    "S-001: 回收(P-001) -> 检测(A-001) -> 转化(B-001) -> 调配(M-001) -> 出口(S-003)",
    "S-002: 回收(P-001) -> 检测(A-001) -> 转化(B-001) -> 调配(M-001) -> 出口(S-003)",
    "S-003: 回收(P-001) -> 检测(A-001) -> 转化(B-001) -> 调配(M-001) -> 出口(S-003)"
  ],
  "message": "model: 溯源链断裂: 货物 S-001 溯源链末节点指向 S-003",
  "ok": false
}
$ echo $?
6
```

S-001 和 S-002 的链末节点都变成了「出口(S-003)」。按 README，一批油脂在调配环节拆成多票出口货物后，每票各自延续一条链，各条链必须持有独立存储、末节点必须是它自己那个出口节点。现在三条链的末节点是同一个。

细看这几处：

- `chains` 是 3，条数没错。
- 每条链的长度也没错，都是 5 个节点。
- 前四个节点（回收 / 检测 / 转化 / 调配）三条链完全一致，这部分本来就是共享前缀，是对的。
- 只有第 5 个节点串了，而且串成的都是**最后**那票货 S-003。
- 报错报的是 S-001，也就是校验时第一条就对不上。

`GET /api/trace` 返回的链也是同样的末节点。

请先不要修改代码。先帮我定位根因，讲清楚为什么三条链的末节点会互相覆盖、为什么覆盖后留下的偏偏是最后一次派生的那票货，以及为什么共享前缀和链条数反而都是正确的，并给出实际执行过的复现命令与观察到的输出作为证据。结论确认后再讨论怎么改。

## 含 Bug 版本

- 仓库：VanceMichael/go-annotation-25
- 仓库地址：https://github.com/VanceMichael/go-annotation-25.git
- parent SHA：a8732a427ce754e4f8e0100fb65450c94f40753f

## 复现步骤

```bash
git clone -- https://github.com/VanceMichael/go-annotation-25.git bug-repro
cd bug-repro
git checkout --detach a8732a427ce754e4f8e0100fb65450c94f40753f
go test ./internal/trace/ -run "TestForkDoesNotShareStorage|TestForkTwiceKeepsBothChains|TestForkThreeWaySplit|TestSplitProducesIndependentChains|TestForkDoesNotMutatePrefix" -count=1
```

## 双架构完整错误信息

### linux/amd64

- 容器内复现预期退出码：1
- 容器内复现实际退出码：1

stdout：

```text
$ go test ./internal/trace/ -run "TestForkDoesNotShareStorage|TestForkTwiceKeepsBothChains|TestForkThreeWaySplit|TestSplitProducesIndependentChains|TestForkDoesNotMutatePrefix" -count=1
--- FAIL: TestForkDoesNotShareStorage (0.00s)
    trace_test.go:40: 第一条链末节点 = S-002, 期望 S-001（不应被第二条覆盖）
--- FAIL: TestForkTwiceKeepsBothChains (0.00s)
    trace_test.go:58: 两条独立链应校验通过: model: 溯源链断裂: 货物 S-001 溯源链末节点指向 S-002
--- FAIL: TestForkThreeWaySplit (0.00s)
    trace_test.go:82: 第 0 条链末节点 = S-003, 期望 S-001
--- FAIL: TestSplitProducesIndependentChains (0.00s)
    trace_test.go:108: Split 产出的链应校验通过: model: 溯源链断裂: 货物 S-001 溯源链末节点指向 S-002
FAIL
FAIL	wasteoil/internal/trace	0.030s
FAIL

```

stderr：

```text
(empty)
```

### linux/arm64

- 容器内复现预期退出码：1
- 容器内复现实际退出码：1

stdout：

```text
$ go test ./internal/trace/ -run "TestForkDoesNotShareStorage|TestForkTwiceKeepsBothChains|TestForkThreeWaySplit|TestSplitProducesIndependentChains|TestForkDoesNotMutatePrefix" -count=1
--- FAIL: TestForkDoesNotShareStorage (0.00s)
    trace_test.go:40: 第一条链末节点 = S-002, 期望 S-001（不应被第二条覆盖）
--- FAIL: TestForkTwiceKeepsBothChains (0.00s)
    trace_test.go:58: 两条独立链应校验通过: model: 溯源链断裂: 货物 S-001 溯源链末节点指向 S-002
--- FAIL: TestForkThreeWaySplit (0.00s)
    trace_test.go:82: 第 0 条链末节点 = S-003, 期望 S-001
--- FAIL: TestSplitProducesIndependentChains (0.00s)
    trace_test.go:108: Split 产出的链应校验通过: model: 溯源链断裂: 货物 S-001 溯源链末节点指向 S-002
FAIL
FAIL	wasteoil/internal/trace	0.002s
FAIL

```

stderr：

```text
(empty)
```

## 通过条件

目标仓库零改动（git status 干净，无新增、修改或删除文件）。
准确指出出问题的 Go 文件与具体符号。
说明从同一前缀派生多条链时新节点为什么被写入同一段存储，并解释这一点如何使先派生的链的末节点被后派生的链覆盖，最终三条链的末节点都等于最后一次派生的出口节点。
解释为什么链条数、链长度与共享前缀不受影响，从而说明偏差只出现在末节点上；并说明为什么校验首个报错指向 S-001。
给出实际执行过的复现命令与观察到的输出作为证据，而非仅凭阅读代码推断。
