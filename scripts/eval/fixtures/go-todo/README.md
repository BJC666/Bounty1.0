# go-todo

简单的待办事项 CLI。

用法：

    go run . add "写周报"
    go run . list
    go run . done 1
    go run . pending

代码结构：

- `main.go` — CLI 入口（子命令 add / list / done / pending）
- `internal/store` — Todo 存储（Add / List / Done / Pending）
- `internal/validate` — 标题校验（ValidateTitle）
- `internal/format` — 列表格式化（FormatList）
