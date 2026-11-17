# Notes

## 本地编译与部署

分支 `local/dshlike` 的本地构建产物固定为仓库根目录的 `crush-dshlike.exe`，
部署目标是 PATH 上的 `<部署目录>\crush.exe`。

环境变量沿用 `Taskfile.yaml` 的全局设置（`CGO_ENABLED=0`、
`GOEXPERIMENT=greenteagc`），构建必须带 `-s -w` 去符号表（否则体积从
~94MB 涨到 ~132MB）。

### 编译

```bash
CGO_ENABLED=0 GOEXPERIMENT=greenteagc go build \
  -ldflags="-s -w -X github.com/charmbracelet/crush/internal/version.Version=$(git describe --long)" \
  -o crush-dshlike.exe .
```

### 验证

```bash
./crush-dshlike.exe --version   # 应输出 crush version <git describe --long>+dirty
```

### 部署

Windows 不允许写正在运行的 exe，所以覆盖前必须先把旧文件改名（改名是允许的，
运行中的进程会继续用改名后的映像）。备份命名沿用
`crush.exe.bak-yyyyMMdd-HHmmss`：

```powershell
Set-Location -LiteralPath '<部署目录>'
$stamp = Get-Date -Format 'yyyyMMdd-HHmmss'
if (Get-Process crush -ErrorAction SilentlyContinue) {
    Rename-Item -LiteralPath '.\crush.exe' -NewName "crush.exe.bak-$stamp" -Force
}
Copy-Item -LiteralPath '<fork 仓库目录>\crush-dshlike.exe' -Destination '.\crush.exe' -Force
& .\crush.exe --version
Get-ChildItem -Filter 'crush*.exe*' | Select-Object Name,Length,LastWriteTime
```

### 收尾

- 部署后**必须重启 Crush** 才生效：正在运行的进程仍然执行改名前的旧映像。
- 确认新 `crush.exe` 的 `Length` / `LastWriteTime` 与刚构建的产物一致。
- 旧备份按需手动清理，`crush.exe.bak-*` 会一直留在部署目录里。
