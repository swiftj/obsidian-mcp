// Package udiff produces unified diffs of two texts, line by line.
package udiff

import (
	"fmt"
	"strings"
)

// Context is the number of unchanged lines shown around each change.
const Context = 3

// maxEdits bounds the Myers search. Beyond it the diff falls back to
// replacing every line, which is correct but not minimal.
const maxEdits = 4000

type opKind byte

const (
	opEq  opKind = ' '
	opDel opKind = '-'
	opIns opKind = '+'
)

type op struct {
	kind opKind
	a, b int // line indexes in old and new
}

// Unified returns a unified diff from old to new, labelled with name, or ""
// when they are equal. A nil old means the file is new.
func Unified(name string, old, new []byte) string {
	a, b := splitLines(string(old)), splitLines(string(new))
	ops := diff(a, b)
	var sb strings.Builder
	from := "a/" + name
	if old == nil {
		from = "/dev/null"
	}
	header := false
	for _, h := range hunks(ops) {
		if !header {
			fmt.Fprintf(&sb, "--- %s\n+++ b/%s\n", from, name)
			header = true
		}
		writeHunk(&sb, h, a, b)
	}
	return sb.String()
}

// splitLines splits s into lines, each keeping its line ending.
func splitLines(s string) []string {
	if s == "" {
		return nil
	}
	lines := strings.SplitAfter(s, "\n")
	if lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	return lines
}

// diff returns the edit script from a to b using Myers' algorithm.
func diff(a, b []string) []op {
	n, m := len(a), len(b)
	total := n + m
	if total == 0 {
		return nil
	}
	off := total + 1
	v := make([]int, 2*total+3)
	var trace [][]int // trace[d] holds v[-d..d] before step d
	for d := 0; d <= total; d++ {
		if d > maxEdits {
			return replaceAll(n, m)
		}
		snap := make([]int, 2*d+1)
		copy(snap, v[off-d:off+d+1])
		trace = append(trace, snap)
		for k := -d; k <= d; k += 2 {
			var x int
			if k == -d || (k != d && v[off+k-1] < v[off+k+1]) {
				x = v[off+k+1]
			} else {
				x = v[off+k-1] + 1
			}
			y := x - k
			for x < n && y < m && a[x] == b[y] {
				x, y = x+1, y+1
			}
			v[off+k] = x
			if x >= n && y >= m {
				return backtrack(trace, n, m)
			}
		}
	}
	return replaceAll(n, m)
}

func backtrack(trace [][]int, n, m int) []op {
	var rev []op
	x, y := n, m
	for d := len(trace) - 1; d >= 0; d-- {
		snap := trace[d]
		at := func(k int) int { return snap[k+d] }
		k := x - y
		var px, py int
		if d > 0 {
			pk := k - 1
			if k == -d || (k != d && at(k-1) < at(k+1)) {
				pk = k + 1
			}
			px = at(pk)
			py = px - pk
		}
		for x > px && y > py {
			x, y = x-1, y-1
			rev = append(rev, op{opEq, x, y})
		}
		if d > 0 {
			if x == px {
				rev = append(rev, op{opIns, px, py})
			} else {
				rev = append(rev, op{opDel, px, py})
			}
		}
		x, y = px, py
	}
	out := make([]op, len(rev))
	for i, o := range rev {
		out[len(rev)-1-i] = o
	}
	return out
}

func replaceAll(n, m int) []op {
	out := make([]op, 0, n+m)
	for i := range n {
		out = append(out, op{opDel, i, 0})
	}
	for j := range m {
		out = append(out, op{opIns, n, j})
	}
	return out
}

// hunks groups ops into hunks with Context lines around each change.
func hunks(ops []op) [][]op {
	var out [][]op
	i := 0
	for i < len(ops) {
		for i < len(ops) && ops[i].kind == opEq {
			i++
		}
		if i == len(ops) {
			break
		}
		start := max(i-Context, 0)
		end := i
		for end < len(ops) {
			if ops[end].kind != opEq {
				end++
				continue
			}
			run := end
			for run < len(ops) && ops[run].kind == opEq {
				run++
			}
			if run == len(ops) || run-end > 2*Context {
				end = min(end+Context, run)
				break
			}
			end = run
		}
		out = append(out, ops[start:end])
		i = end
	}
	return out
}

func writeHunk(sb *strings.Builder, h []op, a, b []string) {
	aStart, bStart := h[0].a, h[0].b
	var aLen, bLen int
	for _, o := range h {
		if o.kind != opIns {
			aLen++
		}
		if o.kind != opDel {
			bLen++
		}
	}
	fmt.Fprintf(sb, "@@ -%s +%s @@\n", rangeOf(aStart, aLen), rangeOf(bStart, bLen))
	for _, o := range h {
		line := ""
		switch o.kind {
		case opDel, opEq:
			line = a[o.a]
		case opIns:
			line = b[o.b]
		}
		sb.WriteByte(byte(o.kind))
		sb.WriteString(line)
		if !strings.HasSuffix(line, "\n") {
			sb.WriteString("\n\\ No newline at end of file\n")
		}
	}
}

func rangeOf(start, n int) string {
	if n == 0 {
		return fmt.Sprintf("%d,0", start)
	}
	if n == 1 {
		return fmt.Sprintf("%d", start+1)
	}
	return fmt.Sprintf("%d,%d", start+1, n)
}
