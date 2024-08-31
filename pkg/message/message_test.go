package message

import (
	"testing"
	"testing/quick"
)

type zeroReader struct{}

func (zeroReader) Read(p []byte) (n int, err error) {
	for n = 0; n < len(p); n++ {
		p[n] = 0
	}
	return
}

type readArgs struct {
	BufferSize int
	Offset     int
	Count      uint16
}

func TestDecoderRead(t *testing.T) {
	var zr zeroReader
	f := func(args readArgs) bool {
		var d Decoder
		if args.BufferSize < 0 {
			args.BufferSize = -args.BufferSize
		}
		args.BufferSize %= 1024 * 1024
		if args.Offset < 0 {
			args.Offset = -args.Offset
		}
		d.buf = make([]byte, args.BufferSize)
		d.off = args.Offset % args.BufferSize
		d.read(zr, args.Count)
		return true
	}
	err := quick.Check(f, &quick.Config{MaxCount: 10000})
	if err != nil {
		t.Fatal(err)
	}
}
