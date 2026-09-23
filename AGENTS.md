# Git 提交信息约定

本项目使用 Conventional Commits 格式：

```text
<type>[optional scope][!]: <short summary>

<optional body>

<optional footer>
```

- `type` 必填；需要 `scope` 时写成 `(scope)`。正文和页脚按需填写，没有不兼容变更时省略 `!`。
- 标题应简短、具体，说明本次提交做了什么。
- 修改较复杂时，在标题后空一行写正文，说明修改原因和关键影响。
- 不兼容变更使用 `!`，或在页脚写 `BREAKING CHANGE: <description>`。

常用类型：

| 类型 | 用途 |
| --- | --- |
| `feat` | 新功能 |
| `fix` | 修复问题 |
| `docs` | 仅修改文档 |
| `refactor` | 重构，功能行为不变 |
| `test` | 增加或修改测试 |
| `build` | 构建流程、依赖 |
| `ci` | 持续集成配置 |
| `chore` | 其他维护工作 |

示例：

```text
build: track Wails build inputs and ignore generated assets
fix(session): restore system proxy after startup failure
feat(profile)!: change profile import format
```
