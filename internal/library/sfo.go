package library

import (
	"bytes"
	"encoding/binary"
	"errors"
	"os"
)

// ReadSFO parses a PlayStation PARAM.SFO file and returns its string and
// integer values keyed by name (TITLE, TITLE_ID, CATEGORY, ...).
func ReadSFO(path string) (map[string]string, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if len(b) < 20 || !bytes.Equal(b[:4], []byte("\x00PSF")) {
		return nil, errors.New("PARAM.SFO inválido")
	}
	le := binary.LittleEndian
	keyTable := int(le.Uint32(b[8:]))
	dataTable := int(le.Uint32(b[12:]))
	count := int(le.Uint32(b[16:]))

	out := map[string]string{}
	for i := 0; i < count; i++ {
		e := 20 + i*16
		if e+16 > len(b) {
			break
		}
		keyOff := keyTable + int(le.Uint16(b[e:]))
		fmtType := le.Uint16(b[e+2:])
		dataLen := int(le.Uint32(b[e+4:]))
		dataOff := dataTable + int(le.Uint32(b[e+12:]))
		if keyOff >= len(b) || dataOff+dataLen > len(b) {
			continue
		}
		end := bytes.IndexByte(b[keyOff:], 0)
		if end < 0 {
			continue
		}
		key := string(b[keyOff : keyOff+end])
		data := b[dataOff : dataOff+dataLen]
		switch fmtType {
		case 0x0404: // uint32
			if len(data) >= 4 {
				out[key] = itoa(le.Uint32(data))
			}
		default: // utf-8 strings, possibly NUL terminated
			out[key] = string(bytes.TrimRight(data, "\x00"))
		}
	}
	return out, nil
}

func itoa(v uint32) string {
	if v == 0 {
		return "0"
	}
	var buf [10]byte
	i := len(buf)
	for v > 0 {
		i--
		buf[i] = byte('0' + v%10)
		v /= 10
	}
	return string(buf[i:])
}
