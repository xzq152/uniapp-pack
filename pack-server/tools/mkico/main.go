//go:build ignore

// 生成多尺寸 .ico。
//
// 用法：go run ./tools/mkico/main.go -in assets/icon-512.png -out assets/app.ico -sizes 16,24,32,48,64,128,256
//
// 注意要用「文件路径 main.go」而不是包路径 ./tools/mkico：
// 本文件带 //go:build ignore，按包引用会被构建约束排除。
// 这个 tag 是有意的——它让 go build ./... 不把这个工具编进主程序。
//
// 为什么自己写：本机没有 ImageMagick，项目也坚持零第三方依赖。
// ICO 容器格式很简单——头 + 目录项 + 每张 PNG 原样塞进去即可
// （Vista 起支持 PNG 压缩的 ICO，资源管理器和托盘都能正确显示）。
package main

import (
	"bytes"
	"encoding/binary"
	"flag"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"os"
	"strconv"
	"strings"
)

func main() {
	in := flag.String("in", "", "source png")
	out := flag.String("out", "app.ico", "output ico")
	sizes := flag.String("sizes", "16,24,32,48,64,128,256", "comma separated sizes")
	flag.Parse()

	if *in == "" {
		fmt.Fprintln(os.Stderr, "-in is required")
		os.Exit(1)
	}

	var list []int
	for _, s := range strings.Split(*sizes, ",") {
		n, err := strconv.Atoi(strings.TrimSpace(s))
		if err == nil && n > 0 && n <= 256 {
			list = append(list, n)
		}
	}
	if len(list) == 0 {
		fmt.Fprintln(os.Stderr, "no valid sizes")
		os.Exit(1)
	}

	f, err := os.Open(*in)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	src, _, err := image.Decode(f)
	f.Close()
	if err != nil {
		fmt.Fprintln(os.Stderr, "decode:", err)
		os.Exit(1)
	}

	type entry struct {
		size int
		data []byte
	}
	var entries []entry
	for _, s := range list {
		img := resize(src, s)
		var buf bytes.Buffer
		if err := png.Encode(&buf, img); err != nil {
			fmt.Fprintln(os.Stderr, "encode:", err)
			os.Exit(1)
		}
		entries = append(entries, entry{size: s, data: buf.Bytes()})
	}

	var outBuf bytes.Buffer
	le := binary.LittleEndian
	_ = binary.Write(&outBuf, le, uint16(0)) // reserved
	_ = binary.Write(&outBuf, le, uint16(1)) // type = icon
	_ = binary.Write(&outBuf, le, uint16(len(entries)))

	offset := 6 + 16*len(entries)
	for _, e := range entries {
		dim := byte(e.size)
		if e.size >= 256 {
			dim = 0 // ICO 规范：256 记作 0
		}
		outBuf.WriteByte(dim)                             // width
		outBuf.WriteByte(dim)                             // height
		outBuf.WriteByte(0)                               // palette count
		outBuf.WriteByte(0)                               // reserved
		_ = binary.Write(&outBuf, le, uint16(1))          // planes
		_ = binary.Write(&outBuf, le, uint16(32))         // bpp
		_ = binary.Write(&outBuf, le, uint32(len(e.data)))
		_ = binary.Write(&outBuf, le, uint32(offset))
		offset += len(e.data)
	}
	for _, e := range entries {
		outBuf.Write(e.data)
	}

	if err := os.WriteFile(*out, outBuf.Bytes(), 0o644); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Printf("wrote %s (%d bytes, %d sizes: %v)\n", *out, outBuf.Len(), len(entries), list)
}

// resize 用盒式平均缩放（对整数倍缩小足够干净）。
// 注意要先把 RGBA() 的预乘值还原成非预乘再平均，否则半透明边缘会发黑。
func resize(src image.Image, size int) image.Image {
	b := src.Bounds()
	sw, sh := b.Dx(), b.Dy()
	dst := image.NewNRGBA(image.Rect(0, 0, size, size))

	for dy := 0; dy < size; dy++ {
		sy0 := b.Min.Y + dy*sh/size
		sy1 := b.Min.Y + (dy+1)*sh/size
		if sy1 <= sy0 {
			sy1 = sy0 + 1
		}
		for dx := 0; dx < size; dx++ {
			sx0 := b.Min.X + dx*sw/size
			sx1 := b.Min.X + (dx+1)*sw/size
			if sx1 <= sx0 {
				sx1 = sx0 + 1
			}
			var sumR, sumG, sumB, sumA, n uint64
			for y := sy0; y < sy1; y++ {
				for x := sx0; x < sx1; x++ {
					cr, cg, cb, ca := src.At(x, y).RGBA()
					if ca == 0 {
						n++
						continue
					}
					sumR += uint64(cr) * 0xffff / uint64(ca)
					sumG += uint64(cg) * 0xffff / uint64(ca)
					sumB += uint64(cb) * 0xffff / uint64(ca)
					sumA += uint64(ca)
					n++
				}
			}
			if n == 0 {
				continue
			}
			avgA := uint8(sumA / n / 0x101)
			var rr, gg, bb uint8
			if sumA > 0 {
				rr = uint8(sumR / n / 0x101)
				gg = uint8(sumG / n / 0x101)
				bb = uint8(sumB / n / 0x101)
			}
			dst.SetNRGBA(dx, dy, color.NRGBA{R: rr, G: gg, B: bb, A: avgA})
		}
	}
	return dst
}
