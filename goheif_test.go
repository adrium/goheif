package goheif

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"image"
	"io"
	"io/ioutil"
	"testing"
)

func TestFormatRegistered(t *testing.T) {
	b, err := ioutil.ReadFile("testdata/camel.heic")
	if err != nil {
		t.Fatal(err)
	}

	img, dec, err := image.Decode(bytes.NewReader(b))
	if err != nil {
		t.Fatalf("unable to decode heic image: %s", err)
	}

	if got, want := dec, "heic"; got != want {
		t.Errorf("unexpected decoder: got %s, want %s", got, want)
	}

	if w, h := img.Bounds().Dx(), img.Bounds().Dy(); w != 1596 || h != 1064 {
		t.Errorf("unexpected decoded image size: got %dx%d, want 1596x1064", w, h)
	}
}

// sample-10bit.heic is a public 1024x512 HEVC Main 10 gradient
// (https://99helpers.com/file-types/heic, free for testing).
// Before high-bit-depth planes were unpacked, Decode reported the byte
// stride (YStride 2048) and treated each 16-bit sample as two pixels.
func TestDecode10BitHEIC(t *testing.T) {
	b, err := ioutil.ReadFile("testdata/sample-10bit.heic")
	if err != nil {
		t.Fatal(err)
	}

	prev := SafeEncoding
	defer func() { SafeEncoding = prev }()

	for _, safe := range []bool{false, true} {
		SafeEncoding = safe
		img, err := Decode(bytes.NewReader(b))
		if err != nil {
			t.Fatalf("safe=%v: %v", safe, err)
		}
		ycc, ok := img.(*image.YCbCr)
		if !ok {
			t.Fatalf("safe=%v: got %T, want *image.YCbCr", safe, img)
		}
		if w, h := ycc.Bounds().Dx(), ycc.Bounds().Dy(); w != 1024 || h != 512 {
			t.Fatalf("safe=%v: size %dx%d, want 1024x512", safe, w, h)
		}
		if ycc.YStride != 1024 || ycc.CStride != 512 {
			t.Fatalf("safe=%v: strides Y=%d C=%d, want 1024 and 512", safe, ycc.YStride, ycc.CStride)
		}

		sum := sha256.New()
		sum.Write(ycc.Y)
		sum.Write(ycc.Cb)
		sum.Write(ycc.Cr)
		const want = "dacc5a62b20068d61e8c5958ebd3485e0e316c3ed398782997dddfb67a42f3df"
		if got := hex.EncodeToString(sum.Sum(nil)); got != want {
			t.Fatalf("safe=%v: plane sha256 %s, want %s", safe, got, want)
		}
	}
}

func BenchmarkSafeEncoding(b *testing.B) {
	benchEncoding(b, true)
}

func BenchmarkRegularEncoding(b *testing.B) {
	benchEncoding(b, false)
}

func benchEncoding(b *testing.B, safe bool) {
	b.Helper()

	currentSetting := SafeEncoding
	defer func() {
		SafeEncoding = currentSetting
	}()
	SafeEncoding = safe

	f, err := ioutil.ReadFile("testdata/camel.heic")
	if err != nil {
		b.Fatal(err)
	}
	r := bytes.NewReader(f)

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		Decode(r)
		r.Seek(0, io.SeekStart)
	}
}
