// Package daily creates daily notes and notes from templates the way
// Obsidian's core Daily notes and Templates plugins do, and formats dates
// with the Moment.js tokens they use.
package daily

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// FormatError reports a Moment.js token this package doesn't support. It is
// refused rather than guessed (V-DAILY-01).
type FormatError struct {
	Format string
	Token  string
}

func (e *FormatError) Error() string {
	return fmt.Sprintf("daily: date format %q uses %q, which is not supported; supported tokens: YYYY YY Q M MM MMM MMMM D DD Do DDD DDDD d dd ddd dddd E W WW GG GGGG H HH h hh m mm s ss A a [text]", e.Format, e.Token)
}

// momentLetters are the letters Moment.js reads as tokens. Others are
// copied as written.
const momentLetters = "YMQDdEeWwGgHhkmsSAaXxZzNLly"

// Format formats t with a Moment.js format string, English locale. Text in
// [brackets] is copied as written, as are letters that aren't tokens.
func Format(t time.Time, layout string) (string, error) {
	var sb strings.Builder
	for i := 0; i < len(layout); {
		c := layout[i]
		switch {
		case c == '[':
			end := strings.IndexByte(layout[i+1:], ']')
			if end < 0 {
				sb.WriteString(layout[i:])
				return sb.String(), nil
			}
			sb.WriteString(layout[i+1 : i+1+end])
			i += end + 2
			continue
		case c == '\\' && i+1 < len(layout):
			sb.WriteByte(layout[i+1])
			i += 2
			continue
		case !strings.ContainsRune(momentLetters, rune(c)):
			sb.WriteByte(c)
			i++
			continue
		}
		j := i
		for j < len(layout) && layout[j] == c {
			j++
		}
		tok := layout[i:j]
		if j < len(layout) && layout[j] == 'o' { // ordinal suffix
			tok += "o"
			j++
		}
		s, ok := formatToken(t, tok)
		if !ok {
			return "", &FormatError{Format: layout, Token: tok}
		}
		sb.WriteString(s)
		i = j
	}
	return sb.String(), nil
}

func pad(n, w int) string {
	s := strconv.Itoa(n)
	for len(s) < w {
		s = "0" + s
	}
	return s
}

func formatToken(t time.Time, tok string) (string, bool) {
	isoYear, isoWeek := t.ISOWeek()
	hour12 := t.Hour() % 12
	if hour12 == 0 {
		hour12 = 12
	}
	switch tok {
	case "YYYY":
		return pad(t.Year(), 4), true
	case "YY":
		return pad(t.Year()%100, 2), true
	case "Q":
		return strconv.Itoa((int(t.Month())-1)/3 + 1), true
	case "M":
		return strconv.Itoa(int(t.Month())), true
	case "MM":
		return pad(int(t.Month()), 2), true
	case "MMM":
		return t.Month().String()[:3], true
	case "MMMM":
		return t.Month().String(), true
	case "D":
		return strconv.Itoa(t.Day()), true
	case "DD":
		return pad(t.Day(), 2), true
	case "Do":
		return ordinal(t.Day()), true
	case "DDD":
		return strconv.Itoa(t.YearDay()), true
	case "DDDD":
		return pad(t.YearDay(), 3), true
	case "d":
		return strconv.Itoa(int(t.Weekday())), true
	case "dd":
		return t.Weekday().String()[:2], true
	case "ddd":
		return t.Weekday().String()[:3], true
	case "dddd":
		return t.Weekday().String(), true
	case "E":
		return strconv.Itoa((int(t.Weekday())+6)%7 + 1), true
	case "W":
		return strconv.Itoa(isoWeek), true
	case "WW":
		return pad(isoWeek, 2), true
	case "GG":
		return pad(isoYear%100, 2), true
	case "GGGG":
		return pad(isoYear, 4), true
	case "H":
		return strconv.Itoa(t.Hour()), true
	case "HH":
		return pad(t.Hour(), 2), true
	case "h":
		return strconv.Itoa(hour12), true
	case "hh":
		return pad(hour12, 2), true
	case "m":
		return strconv.Itoa(t.Minute()), true
	case "mm":
		return pad(t.Minute(), 2), true
	case "s":
		return strconv.Itoa(t.Second()), true
	case "ss":
		return pad(t.Second(), 2), true
	case "A":
		if t.Hour() < 12 {
			return "AM", true
		}
		return "PM", true
	case "a":
		if t.Hour() < 12 {
			return "am", true
		}
		return "pm", true
	}
	return "", false
}

func ordinal(n int) string {
	suffix := "th"
	if n%100 < 11 || n%100 > 13 {
		switch n % 10 {
		case 1:
			suffix = "st"
		case 2:
			suffix = "nd"
		case 3:
			suffix = "rd"
		}
	}
	return strconv.Itoa(n) + suffix
}
