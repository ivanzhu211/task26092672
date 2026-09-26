# probe

品牌素材库入库用的尺寸探测库：只读文件头部结构（不解码像素），
返回素材的「显示尺寸」（CSS px，1in = 96px = 72pt），供 logo 最小
数字宽度（48px）这类规则做校验。

```go
result, err := probe.Probe(r io.ReaderAt, size int64)
```

- 支持格式：PNG（含 CgBI 变体）、JPEG（Exif Orientation）、
  经典 TIFF（II/MM）、SVG、EPS（含 DOS 二进制变体与 `(atend)`）。
- 单文件最大 200MB；单次 Probe 通过 ReaderAt 读取不超过 1MiB，
  超出返回 `ErrBudgetExceeded`。
- 错误可用 `errors.Is` 区分：`ErrUnsupported`（不支持的格式或变体）、
  `ErrCorrupt`（结构损坏或截断）、`ErrNoIntrinsicSize`（SVG 无固有尺寸）、
  `ErrBudgetExceeded`（超出读取预算）。
- 不使用任何图像解码库，不调用外部命令。

```sh
go test ./...
```
