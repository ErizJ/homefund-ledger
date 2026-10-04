// genicon 生成桌面版图标 desktop/icon.ico（蓝底白色"住"字，多尺寸 PNG 压缩）。
// 用法：go run . <字体.ttf> [输出.ico]，默认输出 icon.ico。
package main

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/color"
	"image/png"
	"log"
	"os"

	"golang.org/x/image/draw"
	"golang.org/x/image/font"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/math/fixed"
)

// roundedFill 画圆角矩形
func roundedFill(img *image.RGBA, radius int, c color.RGBA) {
	w, h := img.Bounds().Dx(), img.Bounds().Dy()
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			// 圆角判断：四个角点到圆心的距离
			cx, cy := x, y
			if x < radius {
				cx = radius
			} else if x >= w-radius {
				cx = w - 1 - radius
			}
			if y < radius {
				cy = radius
			} else if y >= h-radius {
				cy = h - 1 - radius
			}
			dx, dy := x-cx, y-cy
			if dx*dx+dy*dy <= radius*radius {
				img.SetRGBA(x, y, c)
			}
		}
	}
}

// render 渲染指定尺寸的图标
func render(size int, face font.Face, ch string) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, size, size))
	// 蓝底圆角（半径随尺寸缩放）
	roundedFill(img, size*56/256, color.RGBA{R: 0x25, G: 0x63, B: 0xEB, A: 0xFF})

	d := &font.Drawer{
		Dst:  img,
		Src:  image.NewUniform(color.White),
		Face: face,
	}
	adv := d.MeasureString(ch)
	m := face.Metrics()
	height := m.Ascent + m.Descent
	d.Dot = fixed.Point26_6{
		X: (fixed.I(size) - adv) / 2,
		Y: fixed.I(size)/2 + height/2 - m.Descent,
	}
	d.DrawString(ch)
	return img
}

func main() {
	fontPath := "NotoSansSC.ttf"
	if len(os.Args) > 1 {
		fontPath = os.Args[1]
	}
	outPath := "icon.ico"
	if len(os.Args) > 2 {
		outPath = os.Args[2]
	}

	fontData, err := os.ReadFile(fontPath)
	if err != nil {
		log.Fatalf("读取字体失败: %v", err)
	}
	parsed, err := opentype.Parse(fontData)
	if err != nil {
		log.Fatalf("解析字体失败: %v", err)
	}
	face, err := opentype.NewFace(parsed, &opentype.FaceOptions{Size: 150, DPI: 72, Hinting: font.HintingFull})
	if err != nil {
		log.Fatalf("创建字体面失败: %v", err)
	}

	base := render(256, face, "住")
	sizes := []int{256, 48, 32, 16}

	// ICO 头 + 各尺寸条目（PNG 压缩存储，Vista+ 支持）
	var buf bytes.Buffer
	binary.Write(&buf, binary.LittleEndian, uint16(0)) // reserved
	binary.Write(&buf, binary.LittleEndian, uint16(1)) // type: icon
	binary.Write(&buf, binary.LittleEndian, uint16(len(sizes)))
	offset := 6 + 16*len(sizes)

	var pngs [][]byte
	for _, s := range sizes {
		img := image.Image(base)
		if s != 256 {
			scaled := image.NewRGBA(image.Rect(0, 0, s, s))
			draw.CatmullRom.Scale(scaled, scaled.Bounds(), base, base.Bounds(), draw.Over, nil)
			img = scaled
		}
		var p bytes.Buffer
		if err := png.Encode(&p, img); err != nil {
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
	log.Printf("已生成 %s（%d 个尺寸）", outPath, len(sizes))
}
