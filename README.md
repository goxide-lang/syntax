# Goxide syntax

Goxide 的公共语法工具，Go module 为 `github.com/goxide-lang/syntax`，需要 Go 1.27。当前提供手写 Go 的 `token`、`proc_macro` 和 `syn`，仅依赖 Go 标准库；不需要先运行 Goxide 编译器。

| 包 | 用途 |
| --- | --- |
| `token` | 不可变 token 树、Goxide 词法解析、构造、诊断位置、绑定上下文和调用内引用表 |
| `proc_macro` | 宏作者的 Context、Output、诊断，以及一次调用的 JSON 编解码和静态入口分派 |
| `syn` | 单个 type/struct/enum 声明的只读语法视图、字段、变体及保留原 token 的回放 |

编译器接入合同见 [RFC 0030](../goxide/docs/rfcs/0030-procedural-macros.md)（工作区相邻 checkout）。转换桥、源码登记、依赖角色构建、协议内容核验及项目 CLI 的受限 WASI 宿主已完成本地联调；浏览器宿主与 `quote` 尚未实现。

## 普通 Go 工具

离线使用 `token` 时可以使用零值上下文：

```go
package main

import (
	"fmt"

	"github.com/goxide-lang/syntax/token"
)

func main() {
	stream, err := token.ParseText("r#match::value + 1;", token.BindingContext{})
	if err != nil {
		panic(err)
	}
	for _, node := range stream.Tokens() {
		fmt.Println(node.Kind(), node.Text())
	}
}
```

`ParseText` 只做词法分析与括号分组，包括 Goxide 的自动分号规则，不解析表达式、类型或声明。token 保留 raw identifier 标志、五类字面量的原始拼写、完整复合标点、显式/隐式分号和三类分组。普通注释不进入公共树，但换行仍参与分号插入。

`Tokens()` 返回副本；`Append`、`WithSpan` 和 `WithDelimiterSpans` 不修改原对象。`Display()` 只提供可读文本，不是保留绑定、原始来源、注释或空白的序列化方法。

## 宏作者与调用方

宏 handler 是普通 Go 函数；三个入口形状为：

```go
func identity(ctx pm.Context, input token.TokenStream) pm.Output {
	return pm.Success(input)
}

func keep(ctx pm.Context, args, item token.TokenStream) pm.Output {
	return pm.Success(item)
}

func answer(ctx pm.Context, item token.TokenStream) pm.Output {
	return pm.FromParse(token.ParseText(
		"func Answer() int { return 42 }", ctx.DefSite(),
	))
}
```

上例的 `pm` 是 `github.com/goxide-lang/syntax/proc_macro` 的导入别名。`Context` 提供定义点/调用点绑定和诊断位置、冻结的目标平台/features，以及宿主已提供的 `RuntimeRoot`。查询不存在的运行库根会返回错误，不加载依赖。SDK 没有环境、文件、网络、类型查询或动态登记入口。

调用方创建 `token.Scope` 和两个绑定上下文，用 `pm.NewRequest(scope, config)` 捕获输入并冻结引用表。随后通过 `WriteRequest` 发出一个完整 JSON 文档；宏 command 使用 `ReadRequest`、`Dispatch` 和 `WriteResponse`；调用方用原 Request 调用 `ReadResponse` 验证响应。可运行的普通 Go 示例见 [example_test.go](proc_macro/example_test.go)，独立 command 见 [testdata/command/main.go](proc_macro/testdata/command/main.go)。

`Dispatch` 接受固定的 `[]pm.Entry`，每项只能设置 Function、Attribute、Derive 中的一个 handler。重复入口、种类不匹配和未知入口会返回错误。SDK 不捕获 handler panic，执行失败由宿主处理。

## 声明解析

`syn.Parse` 接受一个声明的 TokenStream，提供名称、属性、显式可见性、泛型和 where 原文，以及结构体字段或 enum 变体。字段保留多名声明、嵌入形式和 tag；变体区分 unit、tuple 和具名字段。分组 type 声明及其他 item 当前返回带位置的错误。

各节点的 `ToTokens()` 返回原 token，保留绑定、Span、自动分号及片段引用。`Span()` 是首 token 的诊断锚。类型视图只描述写出的语法，不解析别名或查询 impl；`StructFields()` 只查看直接写出的 struct。属性参数、数组长度、函数签名组和 interface body 保留为 token，由编译器完成其余语法与语义检查。

## 引用、诊断与边界

- `Scope` 由调用方在执行前构建，每次调用使用唯一 ID。`ScopeData` 只用于宿主到宏的快照，不能接受宏提交的新快照来替换宿主引用表。
- 原输入可按引用转交。新建 token 的零值上下文在响应编码时绑定到定义点；显式调用点需要 `ctx.CallSite()`。修改诊断 Span 不改变绑定或授予源码身份。
- Fragment 的类别和真实 AST 由编译器保存，SDK 只持有不透明引用与可观察视图。原样转交 Fragment 保留类别；`Children()` 返回的视图没有原引用，重新输出它属于新语法。普通类型、权限及输出位置检查仍由编译器执行。
- 消息上限为 8 MiB，诊断规范 JSON 上限为 64 KiB。token 树限制结构深度 128、100,000 节点/词法 token 与 8 MiB 拼写；引用按展开后大小计费，快照另有物理存储限制。文本扫描计入注释、自动分号和 EOF 的分配，并提前停止超限输入。
- JSON 拒绝重复/未知字段、错误大小写、非法 Unicode、越界引用、错误节点形状与尾随内容。任一 error 诊断使 `ReadResponse` 返回 `DiagnosticError`，保留有序诊断但不返回可用 token。

这些检查验证语法传输，不提供进程隔离。WASI 的文件、网络、时间、内存、总执行时限及浏览器取消策略属于编译器宿主。

## 开发检查

```sh
GOTOOLCHAIN=local GOWORK=off go build ./...
GOTOOLCHAIN=local GOWORK=off go test -race -count=1 ./...
GOTOOLCHAIN=local GOWORK=off go vet ./...
gofmt -l .
```

测试包含真实 native 宏 command 及其生成 Go 的 build/run/test。安装支持 `node:wasi` 的 Node.js 后，可显式运行 WASI 对照：

```sh
GOXIDE_TEST_WASI=1 GOTOOLCHAIN=local GOWORK=off go test -count=1 -run '^TestCommandRoundTrip$' -v ./proc_macro
```

它检查 SDK 在 Go WASI command 中的请求/响应机制，不代替 RFC 的受限宿主与完整 Goxide 过程宏验收。

本仓库使用 [MPL-2.0](LICENSE)。
