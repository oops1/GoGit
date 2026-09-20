package ops

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"slices"
	"strings"

	"github.com/oops1/gogit/internal/gitcore/hash"
	"github.com/oops1/gogit/internal/gitcore/refs"
	"github.com/oops1/gogit/internal/gitcore/repo"
)

const (
	bisectNamesFile       = "BISECT_NAMES"
	bisectTermsFile       = "BISECT_TERMS"
	bisectExpectedRevFile = "BISECT_EXPECTED_REV"
	bisectAncestorsFile   = "BISECT_ANCESTORS_OK"
	bisectBadRef          = refs.BisectPrefix + "bad"
	bisectGoodRefPrefix   = refs.BisectPrefix + "good-"
	bisectSkipRefPrefix   = refs.BisectPrefix + "skip-"
	bisectTermBad         = "bad"
	bisectTermGood        = "good"
	bisectTermSkip        = "skip"
)

type BisectMark string

const (
	BisectGood BisectMark = bisectTermGood
	BisectBad  BisectMark = bisectTermBad
	BisectSkip BisectMark = bisectTermSkip
)

type bisectMarks struct {
	bad     hash.ObjectID
	hasBad  bool
	good    []hash.ObjectID
	skipped []hash.ObjectID
}

func (m bisectMarks) isGood(id hash.ObjectID) bool {
	_, found := slices.BinarySearchFunc(m.good, id, hash.ObjectID.Compare)
	return found
}

func (m bisectMarks) isSkipped(id hash.ObjectID) bool {
	_, found := slices.BinarySearchFunc(m.skipped, id, hash.ObjectID.Compare)
	return found
}

func (m bisectMarks) ready() bool { return m.hasBad && len(m.good) > 0 }

func readBisectMarks(store *refs.Store) (bisectMarks, error) {
	var marks bisectMarks
	for ref, err := range store.Prefix(refs.BisectPrefix) {
		if err != nil {
			return bisectMarks{}, err
		}
		switch name := ref.Name.String(); {
		case name == bisectBadRef:
			marks.bad, marks.hasBad = ref.Target, true
		case strings.HasPrefix(name, bisectGoodRefPrefix):
			marks.good = append(marks.good, ref.Target)
		case strings.HasPrefix(name, bisectSkipRefPrefix):
			marks.skipped = append(marks.skipped, ref.Target)
		}
	}
	slices.SortFunc(marks.good, hash.ObjectID.Compare)
	slices.SortFunc(marks.skipped, hash.ObjectID.Compare)
	return marks, nil
}

func bisectRefOf(mark BisectMark, id hash.ObjectID) refs.Name {
	if mark == BisectBad {
		return refs.Name(bisectBadRef)
	}
	return refs.Name(refs.BisectPrefix + string(mark) + "-" + id.String())
}

func appendStateFile(r *repo.Repository, name, text string) error {
	file, err := fsRootOpenFile(r.Root(), name, os.O_WRONLY|os.O_CREATE|os.O_APPEND, stateFileMode)
	if err != nil {
		return fmt.Errorf("ops: append %s: %w", name, err)
	}
	if _, err := file.WriteString(text); err != nil {
		_ = fsFileClose(file)
		return fmt.Errorf("ops: append %s: %w", name, err)
	}
	if err := fsFileClose(file); err != nil {
		return fmt.Errorf("ops: append %s: %w", name, err)
	}
	return nil
}

func BisectLog(r *repo.Repository) (string, error) {
	text, err := readStateFile(r, bisectLogFile)
	if err != nil {
		return "", err
	}
	if text == "" {
		return "", ErrNotBisecting
	}
	return text, nil
}

func bisectStarted(r *repo.Repository) (string, error) {
	present, err := stateFileExists(r, bisectLogFile)
	if err != nil {
		return "", err
	}
	start, err := readStateFile(r, bisectStartFile)
	if err != nil {
		return "", err
	}
	origin := bisectOrigin(start)
	if !present || origin == "" {
		return "", ErrNotBisecting
	}
	return origin, nil
}

func quoteBisectArgs(args []string) string {
	var out strings.Builder
	for _, arg := range args {
		out.WriteString(" '")
		for index := range len(arg) {
			if byteNeedsBisectQuote(arg[index]) {
				out.WriteString("'\\")
				out.WriteByte(arg[index])
				out.WriteByte('\'')
				continue
			}
			out.WriteByte(arg[index])
		}
		out.WriteByte('\'')
	}
	return out.String()
}

func byteNeedsBisectQuote(current byte) bool { return current == '\'' || current == '!' }

func markAncestorsChecked(r *repo.Repository) error {
	return writeStateFile(r, bisectAncestorsFile, "")
}

func ancestorsAlreadyChecked(r *repo.Repository) (bool, error) {
	info, err := fsRootLstat(r.Root(), bisectAncestorsFile)
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("ops: stat %s: %w", bisectAncestorsFile, err)
	}
	return info.Mode().IsRegular(), nil
}
