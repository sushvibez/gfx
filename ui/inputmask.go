package ui

import (
	"strings"
	"unicode"
)

// Selection, and Qt's input masks, on a text field's state.

// Selection is the selected range as rune indices, start <= end (equal when
// nothing is selected).
func (s *InputState) Selection() (start, end int) {
	return min(s.caret, s.anchor), max(s.caret, s.anchor)
}

// SelectedText is the selected text ("" when nothing is selected).
func (s *InputState) SelectedText() string {
	a, b := s.Selection()
	r := []rune(s.Text.Get())
	a, b = min(a, len(r)), min(b, len(r))
	return string(r[a:b])
}

// Select selects the runes from start to end (clamped; the caret goes to
// end, as QML's select(start, end) leaves it).
func (s *InputState) Select(start, end int) {
	n := len([]rune(s.Text.Get()))
	s.anchor = min(max(start, 0), n)
	s.caret = min(max(end, 0), n)
}

// Deselect collapses the selection onto the caret.
func (s *InputState) Deselect() { s.anchor = s.caret }

// Cursor is the caret's rune index.
func (s *InputState) Cursor() int { return s.caret }

// SetCursor moves the caret (clamped), collapsing the selection.
func (s *InputState) SetCursor(i int) {
	s.caret = min(max(i, 0), len([]rune(s.Text.Get())))
	s.anchor = s.caret
}

// SetMask gives the field a Qt input mask ("" removes it): each mask
// character is a slot — 9 a digit, 0 an optional digit, A a letter, a an
// optional letter, N a letter or digit, n optional, X any character, x
// optional, D a digit 1-9, d optional, H a hex digit, h optional, B a binary
// digit, b optional, # a digit or sign — and > uppercases what follows, <
// lowercases, ! stops either; \ makes the next character literal, as any
// other character is. A trailing ";c" names the blank character, which
// this field does not draw. Typed characters fill the slots in order,
// literals are put in for them, and a character a slot does not take is
// refused.
func (s *InputState) SetMask(mask string) {
	if i := strings.LastIndex(mask, ";"); i >= 0 && i == len(mask)-2 {
		mask = mask[:i]
	}
	s.mask = parseMask(mask)
	if s.mask != nil {
		if t, ok := s.mask.format(s.Text.Get(), false); ok {
			s.Text.Set(t)
		}
	}
}

// Acceptable reports whether the text fills every required slot of the
// mask (true without one) — QML's acceptableInput.
func (s *InputState) Acceptable() bool {
	if s.mask == nil {
		return true
	}
	return s.mask.complete(s.Text.Get())
}

// inputMask is a parsed mask.
type inputMask []maskSlot

type maskSlot struct {
	class    byte // 0: a literal
	literal  rune
	optional bool
	caseMode byte // '>' upper, '<' lower, 0 as typed
}

func parseMask(m string) inputMask {
	if m == "" {
		return nil
	}
	var out inputMask
	var mode byte
	r := []rune(m)
	for i := 0; i < len(r); i++ {
		c := r[i]
		switch c {
		case '>', '<':
			mode = byte(c)
			continue
		case '!':
			mode = 0
			continue
		case '\\':
			if i+1 < len(r) {
				i++
				out = append(out, maskSlot{literal: r[i]})
			}
			continue
		}
		if k, ok := maskClasses[c]; ok {
			out = append(out, maskSlot{class: k.class, optional: k.optional, caseMode: mode})
		} else {
			out = append(out, maskSlot{literal: c})
		}
	}
	return out
}

// takes reports whether the slot accepts r.
func (sl maskSlot) takes(r rune) bool {
	switch sl.class {
	case '9':
		return unicode.IsDigit(r)
	case 'a':
		return unicode.IsLetter(r)
	case 'n':
		return unicode.IsLetter(r) || unicode.IsDigit(r)
	case 'x':
		return unicode.IsPrint(r)
	case 'd':
		return r >= '1' && r <= '9'
	case 'h':
		return unicode.Is(unicode.ASCII_Hex_Digit, r)
	case 'b':
		return r == '0' || r == '1'
	case '#':
		return unicode.IsDigit(r) || r == '+' || r == '-'
	}
	return false
}

// format lays the characters of text into the mask: literals are copied
// (a typed literal where one belongs is consumed), each slot takes the next
// character, and growing (typing, not deleting) also puts in the literals
// that follow the last character. ok is false when a character does not
// fit its slot or there are more characters than slots.
func (m inputMask) format(text string, growing bool) (string, bool) {
	in := []rune(text)
	var out []rune
	i := 0
	for _, sl := range m {
		if i >= len(in) {
			if growing && sl.class == 0 {
				out = append(out, sl.literal)
				continue
			}
			break
		}
		if sl.class == 0 {
			if in[i] == sl.literal {
				i++
			}
			out = append(out, sl.literal)
			continue
		}
		r := in[i]
		if !sl.takes(r) {
			return text, false
		}
		switch sl.caseMode {
		case '>':
			r = unicode.ToUpper(r)
		case '<':
			r = unicode.ToLower(r)
		}
		out = append(out, r)
		i++
	}
	if i < len(in) {
		return text, false // more than the mask holds
	}
	return string(out), true
}

// complete reports whether text fills every required slot.
func (m inputMask) complete(text string) bool {
	r := []rune(text)
	for k, sl := range m {
		if sl.class == 0 || sl.optional {
			continue
		}
		if k >= len(r) || !sl.takes(r[k]) {
			return false
		}
	}
	return true
}

// maskClasses maps a mask character to its slot class and whether the slot
// may be left empty.
var maskClasses = map[rune]struct {
	class    byte
	optional bool
}{
	'9': {'9', false}, '0': {'9', true},
	'A': {'a', false}, 'a': {'a', true},
	'N': {'n', false}, 'n': {'n', true},
	'X': {'x', false}, 'x': {'x', true},
	'D': {'d', false}, 'd': {'d', true},
	'H': {'h', false}, 'h': {'h', true},
	'B': {'b', false}, 'b': {'b', true},
	'#': {'#', false},
}
