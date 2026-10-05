// genicon 生成桌面版图标 desktop/icon.ico（VS Code 深色底 + 白色小屋结合铜钱，多尺寸 PNG 压缩）。
// 用法：go run . [输出.ico]，默认输出 icon.ico，并同时生成同名前缀的 -preview.png 预览图。
package main

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/color"
	"image/png"
	"log"
	"math"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/image/draw"
)

var (
	// VS Code 经典深色（编辑器背景色）
	cDark  = color.RGBA{R: 0x1E, G: 0x1E, B: 0x1E, A: 0xFF}
	cWhite = color.RGBA{R: 0xFF, G: 0xFF, B: 0xFF, A: 0xFF}
)

// roundedFill 圆角矩形填充
func roundedFill(img *image.RGBA, x0, y0, x1, y1 int, radius float64, c color.RGBA) {
	for y := y0; y <= y1; y++ {
		for x := x0; x <= x1; x++ {
			hw, hh := float64(x1-x0)/2, float64(y1-y0)/2
			cx := float64(x) - (float64(x0) + hw)
			cy := float64(y) - (float64(y0) + hh)
			dx := math.Abs(cx) - (hw - radius)
			dy := math.Abs(cy) - (hh - radius)
			if dx <= 0 || dy <= 0 {
				img.SetRGBA(x, y, c)
				continue
			}
			if dx*dx+dy*dy <= radius*radius {
				img.SetRGBA(x, y, c)
			}
		}
	}
}

// fillRect 矩形填充
func fillRect(img *image.RGBA, cx, cy, hw, hh float64, c color.RGBA) {
	for y := int(cy - hh); y <= int(cy+hh); y++ {
		for x := int(cx - hw); x <= int(cx+hw); x++ {
			if x >= 0 && y >= 0 && x < img.Bounds().Dx() && y < img.Bounds().Dy() {
				img.SetRGBA(x, y, c)
			}
		}
	}
}

// triangle 等腰三角形扫描线填充（apex 在上）
func triangle(img *image.RGBA, cx, apexY, baseY, halfBase float64, c color.RGBA) {
	H := baseY - apexY
	if H <= 0 {
		return
	}
	for y := int(apexY); y <= int(baseY); y++ {
		t := (float64(y) - apexY) / H
		hw := halfBase * t
		for x := int(cx - hw); x <= int(cx+hw); x++ {
			if x >= 0 && x < img.Bounds().Dx() && y >= 0 && y < img.Bounds().Dy() {
				img.SetRGBA(x, y, c)
			}
		}
	}
}

// fillCircle 实心圆填充
func fillCircle(img *image.RGBA, cx, cy, r float64, c color.RGBA) {
	for y := int(cy - r); y <= int(cy+r); y++ {
		for x := int(cx - r); x <= int(cx+r); x++ {
			if x < 0 || y < 0 || x >= img.Bounds().Dx() || y >= img.Bounds().Dy() {
				continue
			}
			dx, dy := float64(x)-cx, float64(y)-cy
			if dx*dx+dy*dy <= r*r {
				img.SetRGBA(x, y, c)
			}
		}
	}
}

// drawDollar 纯矢量 $ 符号：椭圆弧拼成的 S + 竖杠贯穿，占满铜钱内芯约三分之二
func drawDollar(img *image.RGBA, size int) {
	s := float64(size)
	cx, cy := 0.5*s, 0.64*s
	rx, ry := 0.069*s, 0.043*s
	thk := 0.034 * s

	// 沿椭圆弧密集采样，用圆点印章连成粗笔画
	arc := func(ecx, ecy, a0, a1 float64, steps int) {
		for i := 0; i <= steps; i++ {
			th := a0 + (a1-a0)*float64(i)/float64(steps)
			x := ecx + rx*math.Cos(th)
			y := ecy + ry*math.Sin(th)
			fillCircle(img, x, y, thk/2, cWhite)
		}
	}
	// 上半弧：椭圆心 (cx, cy-ry)，从右端起绕顶部到左侧
	arc(cx, cy-ry, math.Pi/4, 2*math.Pi, 90)
	// 下半弧：椭圆心 (cx, cy+ry)，从右端起绕底部到左侧
	arc(cx, cy+ry, 0, 5*math.Pi/4, 90)
	// 竖杠：贯穿 S，上下微出头
	fillRect(img, cx, cy, thk/2, 0.109*s, cWhite)
}

// drawHouse 白色小屋：三角屋顶 + 圆角屋身；门的位置挖成"铜钱"（白环 + 深色内芯 + 白色 $）
func drawHouse(img *image.RGBA, size int) {
	w, h := size, size
	f := func(v float64) int { return int(v * float64(size)) }

	// 屋顶：顶点 (0.5, 0.14)，底边 y=0.44，半宽 0.36（稍宽出屋身形成屋檐）
	triangle(img, 0.5*float64(w), 0.14*float64(size), 0.44*float64(size), 0.36*float64(w), cWhite)
	// 屋身：x [0.26, 0.74]，y [0.42, 0.86]，圆角 0.045
	roundedFill(img, f(0.26), f(0.42), f(0.74), f(0.86), 0.045*float64(size), cWhite)

	// 铜钱门：圆心 (0.5, 0.64)，外圈白色，内芯深色（环厚 0.30 × 半径）
	coinCX, coinCY, coinR := 0.5*float64(w), 0.64*float64(h), 0.185*float64(size)
	fillCircle(img, coinCX, coinCY, coinR, cWhite)
	fillCircle(img, coinCX, coinCY, coinR*0.70, cDark)

	drawDollar(img, size)
}

// render 渲染指定尺寸的完整图标（深色圆角底 + 小屋）
func render(size int) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, size, size))
	roundedFill(img, 0, 0, size-1, size-1, 0.22*float64(size), cDark)
	drawHouse(img, size)
	return img
}

func main() {
	outPath := "icon.ico"
	if len(os.Args) > 1 {
		outPath = os.Args[1]
	}

	sizes := []int{256, 64, 48, 32, 16}

	// ICO 头
	var buf bytes.Buffer
	binary.Write(&buf, binary.LittleEndian, uint16(0)) // reserved
	binary.Write(&buf, binary.LittleEndian, uint16(1)) // type: icon
	binary.Write(&buf, binary.LittleEndian, uint16(len(sizes)))
	offset := 6 + 16*len(sizes)

	var pngs [][]byte
	for _, s := range sizes {
		// 4 倍超采样渲染再缩小，边缘平滑
		hi := render(s * 4)
		scaled := image.NewRGBA(image.Rect(0, 0, s, s))
		draw.CatmullRom.Scale(scaled, scaled.Bounds(), hi, hi.Bounds(), draw.Over, nil)
		var p bytes.Buffer
		if err := png.Encode(&p, scaled); err != nil {
			log.Fatalf("PNG 编码失败: %v", err)
		}
		pngs = append(pngs, p.Bytes())

		w := byte(s)
		if s == 256 {
			w = 0
		}
		binary.Write(&buf, binary.LittleEndian, w) // width (0=256)
		binary.Write(&buf, binary.LittleEndian, w) // height
		binary.Write(&buf, binary.LittleEndian, byte(0))
		binary.Write(&buf, binary.LittleEndian, byte(0))
		binary.Write(&buf, binary.LittleEndian, uint16(1))  // planes
		binary.Write(&buf, binary.LittleEndian, uint16(32)) // bit count
		binary.Write(&buf, binary.LittleEndian, uint32(len(p.Bytes())))
		binary.Write(&buf, binary.LittleEndian, uint32(offset))
		offset += len(p.Bytes())
	}
	for _, p := range pngs {
		buf.Write(p)
	}
	if err := os.WriteFile(outPath, buf.Bytes(), 0o644); err != nil {
		log.Fatalf("写入图标失败: %v", err)
	}

	// 512 预览图（方便人眼检查）
	preview := render(512)
	var pb bytes.Buffer
	if err := png.Encode(&pb, preview); err != nil {
		log.Fatalf("预览图编码失败: %v", err)
	}
	previewPath := strings.TrimSuffix(outPath, filepath.Ext(outPath)) + "-preview.png"
	if err := os.WriteFile(previewPath, pb.Bytes(), 0o644); err != nil {
		log.Fatalf("写入预览图失败: %v", err)
	}
	log.Printf("已生成 %s（%d 个尺寸）与预览图 %s", outPath, len(sizes), previewPath)
}
