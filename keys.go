package main

import (
	"io"
	"unicode/utf8"
)

// key is one decoded key press: name is up, down, return, escape, backspace, space, ctrl-c or char (with r set).
type key struct {
	name string
	r    rune
}

// keyReader decodes raw terminal input (VT sequences) into key presses.
type keyReader struct {
	in      io.Reader
	pending []key
}

func newKeyReader(in io.Reader) *keyReader { return &keyReader{in: in} }

func (kr *keyReader) next() (key, error) {
	for len(kr.pending) == 0 {
		buf := make([]byte, 256)
		n, err := kr.in.Read(buf)
		if n > 0 {
			kr.pending = parseKeys(buf[:n])
		} else if err != nil {
			return key{}, err
		}
	}
	k := kr.pending[0]
	kr.pending = kr.pending[1:]
	return k, nil
}

// parseKeys splits one read into key presses. A lone ESC at the end of a read is the Esc key;
// ESC followed by [ or O starts an escape sequence (only arrows are recognised; others are skipped).
func parseKeys(b []byte) []key {
	var keys []key
	for i := 0; i < len(b); {
		c := b[i]
		switch {
		case c == 0x1b:
			if i+1 >= len(b) || (b[i+1] != '[' && b[i+1] != 'O') {
				keys = append(keys, key{name: "escape"})
				i++
				continue
			}
			j := i + 2
			for j < len(b) && (b[j] < 0x40 || b[j] > 0x7e) {
				j++
			}
			if j < len(b) {
				switch b[j] {
				case 'A':
					keys = append(keys, key{name: "up"})
				case 'B':
					keys = append(keys, key{name: "down"})
				}
			}
			i = j + 1
		case c == '\r' || c == '\n':
			keys = append(keys, key{name: "return"})
			if c == '\r' && i+1 < len(b) && b[i+1] == '\n' {
				i++
			}
			i++
		case c == 0x7f || c == 0x08:
			keys = append(keys, key{name: "backspace"})
			i++
		case c == 0x03:
			keys = append(keys, key{name: "ctrl-c"})
			i++
		case c == ' ':
			keys = append(keys, key{name: "space", r: ' '})
			i++
		case c < ' ':
			i++
		default:
			r, size := utf8.DecodeRune(b[i:])
			keys = append(keys, key{name: "char", r: r})
			i += size
		}
	}
	return keys
}
