// genicon: 앱 아이콘 PNG 세트를 생성한다 (파란 둥근 사각형 + 흰 다운로드 화살표).
// 사용: go run ./scripts/genicon <출력폴더>
// 이후 iconutil -c icns 로 .icns를 만든다.
package main

import (
	"fmt"
	"image"
	"image/color"
	"image/png"
	"math"
	"os"
	"path/filepath"
)

func main() {
	if len(os.Args) != 2 {
		fmt.Fprintln(os.Stderr, "사용: genicon <출력폴더>")
		os.Exit(2)
	}
	dir := os.Args[1]
	if err := os.MkdirAll(dir, 0o755); err != nil {
		panic(err)
	}
	// macOS iconset 규격
	for _, s := range []struct {
		size int
		name string
	}{
		{16, "icon_16x16.png"}, {32, "icon_16x16@2x.png"},
		{32, "icon_32x32.png"}, {64, "icon_32x32@2x.png"},
		{128, "icon_128x128.png"}, {256, "icon_128x128@2x.png"},
		{256, "icon_256x256.png"}, {512, "icon_256x256@2x.png"},
		{512, "icon_512x512.png"}, {1024, "icon_512x512@2x.png"},
	} {
		img := draw(s.size)
		f, err := os.Create(filepath.Join(dir, s.name))
		if err != nil {
			panic(err)
		}
		if err := png.Encode(f, img); err != nil {
			panic(err)
		}
		f.Close()
	}
	fmt.Println("아이콘 PNG 생성 완료:", dir)
}

func draw(n int) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, n, n))
	f := float64(n)
	// macOS 아이콘 그리드: 전체의 ~80%를 차지하는 둥근 사각형
	margin := f * 0.10
	radius := f * 0.18
	top := color.RGBA{60, 150, 255, 255}
	bottom := color.RGBA{10, 90, 220, 255}

	for y := 0; y < n; y++ {
		for x := 0; x < n; x++ {
			fx, fy := float64(x)+0.5, float64(y)+0.5
			if !inRoundRect(fx, fy, margin, margin, f-margin, f-margin, radius) {
				continue
			}
			t := (fy - margin) / (f - 2*margin)
			c := color.RGBA{
				lerp(top.R, bottom.R, t),
				lerp(top.G, bottom.G, t),
				lerp(top.B, bottom.B, t),
				255,
			}
			if inArrow(fx, fy, f) || inTray(fx, fy, f) {
				c = color.RGBA{255, 255, 255, 255}
			}
			img.SetRGBA(x, y, c)
		}
	}
	return img
}

func lerp(a, b uint8, t float64) uint8 {
	return uint8(float64(a) + (float64(b)-float64(a))*t)
}

func inRoundRect(x, y, x0, y0, x1, y1, r float64) bool {
	if x < x0 || x > x1 || y < y0 || y > y1 {
		return false
	}
	cx := math.Max(x0+r, math.Min(x, x1-r))
	cy := math.Max(y0+r, math.Min(y, y1-r))
	dx, dy := x-cx, y-cy
	return dx*dx+dy*dy <= r*r
}

// 아래로 향하는 화살표 (자루 + 삼각형 머리)
func inArrow(x, y, f float64) bool {
	cx := f / 2
	shaftW := f * 0.11
	shaftTop := f * 0.24
	headTop := f * 0.47
	headBot := f * 0.66
	headW := f * 0.26
	if y >= shaftTop && y <= headTop+f*0.02 && math.Abs(x-cx) <= shaftW/2 {
		return true
	}
	if y >= headTop && y <= headBot {
		t := (y - headTop) / (headBot - headTop)
		return math.Abs(x-cx) <= headW*(1-t)
	}
	return false
}

// 받침 트레이
func inTray(x, y, f float64) bool {
	cx := f / 2
	w := f * 0.46
	yTop := f * 0.72
	yBot := f * 0.78
	return y >= yTop && y <= yBot && math.Abs(x-cx) <= w/2
}
