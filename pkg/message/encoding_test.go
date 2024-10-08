package message

import (
	"bytes"
	"testing"
	"testing/quick"

	"github.com/stretchr/testify/assert"
)

func TestMessageWhatYouEncodeIsWhatYouDecode(t *testing.T) {
	config := &quick.Config{
		MaxCount: 100000,
	}
	t.Run("pack and unpack messages with fresh encoder/decoder", func(t *testing.T) {
		identity := func(m Message) Message {
			return m
		}
		packUnpack := func(in Message) Message {
			var buf bytes.Buffer
			var out Message
			encoder := new(Encoder)
			decoder := new(Decoder)
			assert.Nil(t, encoder.Encode(&buf, in))
			assert.Nil(t, decoder.Decode(&buf, &out))
			return out
		}
		if err := quick.CheckEqual(packUnpack, identity, config); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("pack and unpack messages with shared encoder/decoder", func(t *testing.T) {
		var buf bytes.Buffer
		encoder := new(Encoder)
		decoder := new(Decoder)
		identity := func(m Message) Message {
			return m
		}
		packunpack := func(in Message) Message {
			var out Message
			assert.Nil(t, encoder.Encode(&buf, in))
			assert.Nil(t, decoder.Decode(&buf, &out))
			return out
		}
		if err := quick.CheckEqual(packunpack, identity, config); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("conversion to string", func(t *testing.T) {
		assert.Equal(t,
			"kind=GET tag=42 key=name",
			NewGetMessage(42, "name").String(),
		)
		assert.Equal(t,
			"kind=PUT tag=43 key=name value=mark version=666",
			NewPutMessage(43, "name", "mark", 666).String(),
		)
		assert.Equal(t,
			"kind=ERROR tag=44 value=neutrino...",
			NewErrorMessage(44, "neutrinos hit the memory bank").String(),
		)
		assert.Equal(t,
			"kind=AUTH tag=45 value=true",
			NewAuthMessage(45, "s3cr3t").String(),
		)
		assert.Equal(t,
			"kind=AUTH tag=46 value=false",
			NewAuthMessage(46, "").String(),
		)
	})
}
