package goheif

import (
	"fmt"
	"image"
	"reflect"
	"strings"
	"testing"
)

func TestStitchYCbCrTileUsesVisibleTileWidth(t *testing.T) {
	dst := image.NewYCbCr(image.Rect(0, 0, 8, 2), image.YCbCrSubsampleRatio420)
	src := &image.YCbCr{
		Y:              []uint8{1, 2, 3, 4, 200, 201, 5, 6, 7, 8, 202, 203},
		Cb:             []uint8{9, 10, 204, 205},
		Cr:             []uint8{11, 12, 206, 207},
		YStride:        6,
		CStride:        4,
		SubsampleRatio: image.YCbCrSubsampleRatio420,
		Rect:           image.Rect(0, 0, 4, 2),
	}

	if err := stitchYCbCrTile(dst, src, 1, 0); err != nil {
		t.Fatalf("stitchYCbCrTile returned error: %v", err)
	}

	wantY := []uint8{
		0, 0, 0, 0, 1, 2, 3, 4,
		0, 0, 0, 0, 5, 6, 7, 8,
	}
	if !reflect.DeepEqual(dst.Y, wantY) {
		t.Fatalf("unexpected Y plane: got %v want %v", dst.Y, wantY)
	}

	wantCb := []uint8{0, 0, 9, 10}
	if !reflect.DeepEqual(dst.Cb, wantCb) {
		t.Fatalf("unexpected Cb plane: got %v want %v", dst.Cb, wantCb)
	}

	wantCr := []uint8{0, 0, 11, 12}
	if !reflect.DeepEqual(dst.Cr, wantCr) {
		t.Fatalf("unexpected Cr plane: got %v want %v", dst.Cr, wantCr)
	}
}

func TestStitchYCbCrTileRejectsSubsampleMismatch(t *testing.T) {
	dst := image.NewYCbCr(image.Rect(0, 0, 4, 2), image.YCbCrSubsampleRatio420)
	src := image.NewYCbCr(image.Rect(0, 0, 4, 2), image.YCbCrSubsampleRatio444)

	err := stitchYCbCrTile(dst, src, 0, 0)
	if err == nil {
		t.Fatal("expected stitchYCbCrTile to reject mismatched subsampling")
	}
}

// TestStitchYCbCrTileReportedGridBounds covers the slice bounds from
// adrium/goheif#18 and jdeng/goheif#38: [12583936:12582912].
//
// Those numbers are an 8x6 grid of 512x512 tiles (output Y length
// 4096*3072 = 12582912) indexed with a 1024-byte source stride. That stride
// is what libde265 reports for a 512-wide Main 10 tile before the plane is
// unpacked to 8-bit samples. The old copy used x*YStride as the destination
// offset and panicked on column 5 of the last row.
func TestStitchYCbCrTileReportedGridBounds(t *testing.T) {
	const (
		tileW      = 512
		tileH      = 512
		columns    = 8
		rows       = 6
		srcYStride = 1024
		srcCStride = 512
		placeX     = 5
		placeY     = 5
	)

	dst := image.NewYCbCr(image.Rect(0, 0, tileW*columns, tileH*rows), image.YCbCrSubsampleRatio420)
	if len(dst.Y) != 12582912 {
		t.Fatalf("destination Y length = %d, want 12582912", len(dst.Y))
	}

	yPlane := make([]byte, tileH*srcYStride)
	cbPlane := make([]byte, (tileH/2)*srcCStride)
	crPlane := make([]byte, (tileH/2)*srcCStride)
	yPlane[0] = 40
	yPlane[tileW] = 99 // padding past the visible width; must not be copied
	yPlane[(tileH-1)*srcYStride] = 41
	yPlane[(tileH-1)*srcYStride+tileW-1] = 42
	cbPlane[0] = 50
	crPlane[0] = 60

	src := &image.YCbCr{
		Y:              yPlane,
		Cb:             cbPlane,
		Cr:             crPlane,
		YStride:        srcYStride,
		CStride:        srcCStride,
		SubsampleRatio: image.YCbCrSubsampleRatio420,
		Rect:           image.Rect(0, 0, tileW, tileH),
	}

	// The previous assembly expression panics at the reported bounds.
	oldIndex := (placeY*tileH+tileH-1)*dst.YStride + placeX*src.YStride
	if oldIndex != 12583936 || len(dst.Y) != 12582912 {
		t.Fatalf("repro index = %d len = %d, want 12583936 and 12582912", oldIndex, len(dst.Y))
	}
	func() {
		defer func() {
			rec := recover()
			if rec == nil {
				t.Fatal("expected the old stride offset to panic with the reported bounds")
			}
			msg := fmt.Sprint(rec)
			if !strings.Contains(msg, "12583936") || !strings.Contains(msg, "12582912") {
				t.Fatalf("panic %q does not match the reported bounds", msg)
			}
		}()
		_ = dst.Y[oldIndex:len(dst.Y)]
	}()

	if err := stitchYCbCrTile(dst, src, placeX, placeY); err != nil {
		t.Fatalf("stitchYCbCrTile returned error: %v", err)
	}

	dstX := placeX * tileW
	dstY := placeY * tileH
	if got := dst.Y[dst.YOffset(dstX, dstY)]; got != 40 {
		t.Fatalf("top-left luma = %d, want 40", got)
	}
	if got := dst.Y[dst.YOffset(dstX, dstY+tileH-1)]; got != 41 {
		t.Fatalf("bottom-left luma = %d, want 41", got)
	}
	if got := dst.Y[dst.YOffset(dstX+tileW-1, dstY+tileH-1)]; got != 42 {
		t.Fatalf("bottom-right luma = %d, want 42", got)
	}
	// Padding byte immediately after the visible row must not land in the next pixel.
	if got := dst.Y[dst.YOffset(dstX+tileW, dstY)]; got != 0 {
		t.Fatalf("pixel past the tile copied stride padding: %d", got)
	}
	chromaX := placeX * (tileW / 2)
	chromaY := placeY * (tileH / 2)
	cbAt := chromaY*dst.CStride + chromaX
	if dst.Cb[cbAt] != 50 || dst.Cr[cbAt] != 60 {
		t.Fatalf("chroma at tile origin = %d/%d, want 50/60", dst.Cb[cbAt], dst.Cr[cbAt])
	}
}
