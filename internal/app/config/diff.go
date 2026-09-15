package config

import (
	"strings"

	"github.com/larffxx/singboxui/internal/domain/apperr"
	"github.com/larffxx/singboxui/internal/domain/profile"
)

// diffCellLimit bounds the LCS table; larger inputs fall back to a block diff so
// the UI cannot be made to allocate unbounded memory (spec §56, §83).
const diffCellLimit = 4_000_000

// DiffKind classifies one diff line.
type DiffKind string

const (
	// DiffSame is an unchanged line.
	DiffSame DiffKind = "same"
	// DiffAdded is a line only present on the right side.
	DiffAdded DiffKind = "added"
	// DiffRemoved is a line only present on the left side.
	DiffRemoved DiffKind = "removed"
)

// DiffLine is one line of a side-by-side ready diff.
type DiffLine struct {
	Kind DiffKind `json:"kind"`
	// Left and Right are 1-based line numbers, 0 when the side has no line.
	Left  int    `json:"left"`
	Right int    `json:"right"`
	Text  string `json:"text"`
}

// Diff is the result of comparing two configurations.
type Diff struct {
	Left      string     `json:"leftLabel"`
	Right     string     `json:"rightLabel"`
	Lines     []DiffLine `json:"lines"`
	Added     int        `json:"added"`
	Removed   int        `json:"removed"`
	Identical bool       `json:"identical"`
}

// Compare compares a draft against a revision or two revisions (spec §56).
func (s *Service) Compare(leftJSON, rightJSON, leftLabel, rightLabel string) (Diff, error) {
	left, err := s.normalize(leftJSON)
	if err != nil {
		return Diff{}, err
	}
	right, err := s.normalize(rightJSON)
	if err != nil {
		return Diff{}, err
	}
	diff := DiffJSON(left, right)
	diff.Left = leftLabel
	diff.Right = rightLabel
	return diff, nil
}

// DiffJSON compares two pretty-printed JSON documents line by line.
func DiffJSON(left, right string) Diff {
	leftLines := splitLines(left)
	rightLines := splitLines(right)
	result := Diff{Left: "left", Right: "right"}

	prefix := 0
	for prefix < len(leftLines) && prefix < len(rightLines) && leftLines[prefix] == rightLines[prefix] {
		prefix++
	}
	suffix := 0
	for suffix < len(leftLines)-prefix && suffix < len(rightLines)-prefix &&
		leftLines[len(leftLines)-1-suffix] == rightLines[len(rightLines)-1-suffix] {
		suffix++
	}

	midLeft := leftLines[prefix : len(leftLines)-suffix]
	midRight := rightLines[prefix : len(rightLines)-suffix]

	for i := 0; i < prefix; i++ {
		result.Lines = append(result.Lines, DiffLine{Kind: DiffSame, Left: i + 1, Right: i + 1, Text: leftLines[i]})
	}
	result.Lines = append(result.Lines, diffMiddle(midLeft, midRight, prefix)...)
	for i := 0; i < suffix; i++ {
		leftNo := len(leftLines) - suffix + i + 1
		rightNo := len(rightLines) - suffix + i + 1
		result.Lines = append(result.Lines, DiffLine{Kind: DiffSame, Left: leftNo, Right: rightNo, Text: leftLines[len(leftLines)-suffix+i]})
	}

	for _, line := range result.Lines {
		switch line.Kind {
		case DiffAdded:
			result.Added++
		case DiffRemoved:
			result.Removed++
		}
	}
	result.Identical = result.Added == 0 && result.Removed == 0
	return result
}

// diffMiddle runs an LCS diff over the changed region, or a coarse block diff
// when the region is too large for a table.
func diffMiddle(left, right []string, offset int) []DiffLine {
	if len(left) == 0 && len(right) == 0 {
		return nil
	}
	if len(left)*len(right) > diffCellLimit {
		return blockDiff(left, right, offset)
	}

	table := make([][]int, len(left)+1)
	for i := range table {
		table[i] = make([]int, len(right)+1)
	}
	for i := len(left) - 1; i >= 0; i-- {
		for j := len(right) - 1; j >= 0; j-- {
			if left[i] == right[j] {
				table[i][j] = table[i+1][j+1] + 1
				continue
			}
			if table[i+1][j] >= table[i][j+1] {
				table[i][j] = table[i+1][j]
			} else {
				table[i][j] = table[i][j+1]
			}
		}
	}

	var out []DiffLine
	i, j := 0, 0
	for i < len(left) && j < len(right) {
		switch {
		case left[i] == right[j]:
			out = append(out, DiffLine{Kind: DiffSame, Left: offset + i + 1, Right: offset + j + 1, Text: left[i]})
			i++
			j++
		case table[i+1][j] >= table[i][j+1]:
			out = append(out, DiffLine{Kind: DiffRemoved, Left: offset + i + 1, Text: left[i]})
			i++
		default:
			out = append(out, DiffLine{Kind: DiffAdded, Right: offset + j + 1, Text: right[j]})
			j++
		}
	}
	for ; i < len(left); i++ {
		out = append(out, DiffLine{Kind: DiffRemoved, Left: offset + i + 1, Text: left[i]})
	}
	for ; j < len(right); j++ {
		out = append(out, DiffLine{Kind: DiffAdded, Right: offset + j + 1, Text: right[j]})
	}
	return out
}

func blockDiff(left, right []string, offset int) []DiffLine {
	out := make([]DiffLine, 0, len(left)+len(right))
	for i, line := range left {
		out = append(out, DiffLine{Kind: DiffRemoved, Left: offset + i + 1, Text: line})
	}
	for j, line := range right {
		out = append(out, DiffLine{Kind: DiffAdded, Right: offset + j + 1, Text: line})
	}
	return out
}

func splitLines(text string) []string {
	trimmed := strings.TrimRight(text, "\n")
	if trimmed == "" {
		return nil
	}
	return strings.Split(trimmed, "\n")
}

// CompareRevisions compares two stored revisions of a profile.
func (s *Service) CompareRevisions(left RevisionView, right RevisionView) (Diff, error) {
	leftText, err := s.normalize(left.ConfigJSON)
	if err != nil {
		return Diff{}, err
	}
	rightText, err := s.normalize(right.ConfigJSON)
	if err != nil {
		return Diff{}, err
	}
	diff := DiffJSON(leftText, rightText)
	diff.Left = revisionLabel(left.Revision)
	diff.Right = revisionLabel(right.Revision)
	return diff, nil
}

// CompareWithActive compares arbitrary configuration text with the given
// revision.
func (s *Service) CompareWithActive(draftJSON string, revision RevisionView) (Diff, error) {
	if revision.ID == "" {
		return Diff{}, apperr.New(apperr.CodeRevisionNotFound, "config.CompareWithActive", "there is no revision to compare against")
	}
	return s.Compare(draftJSON, revision.ConfigJSON, "draft", revisionLabel(revision.Revision))
}

func revisionLabel(rev profile.Revision) string {
	short := rev.ID
	if len(short) > 8 {
		short = short[:8]
	}
	return string(rev.Source) + " · " + short
}
