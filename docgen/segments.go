package docgen

import "strings"

type segmentKind int

const (
	// proseSegment is one paragraph of wrapped prose, its lines as written.
	proseSegment segmentKind = iota
	// unwrappedSegment is a click "\b" paragraph: lines kept as written.
	unwrappedSegment
	// exampleSegment is an indented run after a blank line, dedented.
	exampleSegment
)

type helpSegment struct {
	kind  segmentKind
	lines []string
}

// helpSegments splits help text the way both renderers read it. A line that
// is blank after trimming ends a paragraph. Outside "\b" paragraphs, an
// indented run after a blank line is an example; an indented line directly
// after prose is a wrapped continuation and stays in the paragraph.
func helpSegments(value string) []helpSegment {
	lines := strings.Split(strings.TrimSuffix(value, "\n"), "\n")
	blank := func(i int) bool { return strings.TrimSpace(lines[i]) == "" }
	unwrap := func(i int) bool { return strings.TrimSpace(lines[i]) == "\b" }
	out := []helpSegment{}
	for i := 0; i < len(lines); {
		switch {
		case blank(i):
			i++
		case unwrap(i):
			end := i + 1
			for end < len(lines) && !blank(end) {
				end++
			}
			if end > i+1 {
				out = append(out, helpSegment{unwrappedSegment, lines[i+1 : end]})
			}
			i = end
		case indented(lines[i]) && (i == 0 || blank(i-1)):
			run, end := indentedSpan(lines, i)
			out = append(out, helpSegment{exampleSegment, run})
			i = end
		default:
			end := i + 1
			for end < len(lines) && !blank(end) && !unwrap(end) {
				end++
			}
			out = append(out, helpSegment{proseSegment, lines[i:end]})
			i = end
		}
	}
	return out
}
