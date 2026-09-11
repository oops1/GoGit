package ops

import (
	"errors"
	"fmt"
	"io/fs"
	"path"
	"strconv"
	"strings"
	"time"

	"github.com/oops1/gogit/internal/gitcore/hash"
	"github.com/oops1/gogit/internal/gitcore/object"
	"github.com/oops1/gogit/internal/gitcore/repo"
)

const (
	rebaseDir           = "rebase-merge"
	rebaseHeadFile      = "REBASE_HEAD"
	rebaseHeadName      = "head-name"
	rebaseOnto          = "onto"
	rebaseOrigHead      = "orig-head"
	rebaseTodo          = "git-rebase-todo"
	rebaseTodoBackup    = "git-rebase-todo.backup"
	rebaseDone          = "done"
	rebaseMsgNum        = "msgnum"
	rebaseEnd           = "end"
	rebaseAuthorScript  = "author-script"
	rebaseMessage       = "message"
	rebaseStopped       = "stopped-sha"
	rebaseRewritten     = "rewritten-list"
	rebaseInteractive   = "interactive"
	rebaseDropRedundant = "drop_redundant_commits"
	rebaseNoReschedule  = "no-reschedule-failed-exec"
	actionPick          = "pick"
	authorNameKey       = "GIT_AUTHOR_NAME"
	authorEmailKey      = "GIT_AUTHOR_EMAIL"
	authorDateKey       = "GIT_AUTHOR_DATE"
)

var ErrRebaseStateCorrupt = errors.New("ops: the rebase state cannot be read")

type RebaseStep struct {
	Action  string
	Commit  hash.ObjectID
	Subject string
}

func (s RebaseStep) line() string {
	return s.Action + " " + s.Commit.String() + " " + s.Subject + "\n"
}

type RebaseState struct {
	HeadName  string
	Onto      hash.ObjectID
	OrigHead  hash.ObjectID
	Done      []RebaseStep
	Todo      []RebaseStep
	Stopped   hash.ObjectID
	Message   string
	Author    *object.Signature
	Rewritten string
}

func (s RebaseState) InProgress() bool { return s.HeadName != "" }

func rebasePath(name string) string { return path.Join(rebaseDir, name) }

func ReadRebaseState(r *repo.Repository) (RebaseState, error) {
	headName, err := readStateFile(r, rebasePath(rebaseHeadName))
	if err != nil || headName == "" {
		return RebaseState{}, err
	}
	state := RebaseState{HeadName: strings.TrimSpace(headName)}
	for name, into := range map[string]*hash.ObjectID{rebaseOnto: &state.Onto, rebaseOrigHead: &state.OrigHead, rebaseStopped: &state.Stopped} {
		if *into, err = readHeadFile(r, rebasePath(name)); err != nil {
			return RebaseState{}, err
		}
	}
	for name, into := range map[string]*[]RebaseStep{rebaseDone: &state.Done, rebaseTodo: &state.Todo} {
		text, err := readStateFile(r, rebasePath(name))
		if err != nil {
			return RebaseState{}, err
		}
		if *into, err = parseTodo(text); err != nil {
			return RebaseState{}, err
		}
	}
	message, err := readStateFile(r, rebasePath(rebaseMessage))
	if err != nil {
		return RebaseState{}, err
	}
	state.Message = trimMessage(message)
	if state.Rewritten, err = readStateFile(r, rebasePath(rebaseRewritten)); err != nil {
		return RebaseState{}, err
	}
	script, err := readStateFile(r, rebasePath(rebaseAuthorScript))
	if err != nil {
		return RebaseState{}, err
	}
	if state.Author, err = parseAuthorScript(script); err != nil {
		return RebaseState{}, err
	}
	return state, nil
}

func trimMessage(message string) string {
	message = strings.TrimRight(message, "\n")
	if message == "" {
		return ""
	}
	return message + "\n"
}

func parseTodo(text string) ([]RebaseStep, error) {
	var steps []RebaseStep
	for line := range strings.SplitSeq(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.SplitN(line, " ", 3)
		if len(fields) < 2 {
			return nil, fmt.Errorf("%w: %q", ErrRebaseStateCorrupt, line)
		}
		id, err := hash.Parse(fields[1])
		if err != nil {
			return nil, fmt.Errorf("%w: %q: %w", ErrRebaseStateCorrupt, line, err)
		}
		step := RebaseStep{Action: fields[0], Commit: id}
		if len(fields) == 3 {
			step.Subject = fields[2]
		}
		steps = append(steps, step)
	}
	return steps, nil
}

func formatTodo(steps []RebaseStep) string {
	var b strings.Builder
	for _, step := range steps {
		b.WriteString(step.line())
	}
	return b.String()
}

func parseAuthorScript(script string) (*object.Signature, error) {
	if strings.TrimSpace(script) == "" {
		return nil, nil
	}
	values := map[string]string{}
	for line := range strings.SplitSeq(strings.TrimSpace(script), "\n") {
		key, quoted, ok := strings.Cut(line, "=")
		if !ok {
			return nil, fmt.Errorf("%w: author-script %q", ErrRebaseStateCorrupt, line)
		}
		value, err := unquoteShell(quoted)
		if err != nil {
			return nil, err
		}
		values[key] = value
	}
	when, err := parseAuthorDate(values[authorDateKey])
	if err != nil {
		return nil, err
	}
	return &object.Signature{Name: values[authorNameKey], Email: values[authorEmailKey], When: when}, nil
}

func parseAuthorDate(value string) (time.Time, error) {
	stamp, zone, ok := strings.Cut(strings.TrimPrefix(value, "@"), " ")
	seconds, err := strconv.ParseInt(stamp, 10, 64)
	if !ok || err != nil || len(zone) != 5 {
		return time.Time{}, fmt.Errorf("%w: author date %q", ErrRebaseStateCorrupt, value)
	}
	hours, errHours := strconv.Atoi(zone[1:3])
	minutes, errMinutes := strconv.Atoi(zone[3:5])
	if errHours != nil || errMinutes != nil || (zone[0] != '+' && zone[0] != '-') {
		return time.Time{}, fmt.Errorf("%w: author date %q", ErrRebaseStateCorrupt, value)
	}
	offset := (hours*60 + minutes) * 60
	if zone[0] == '-' {
		offset = -offset
	}
	return time.Unix(seconds, 0).In(time.FixedZone("", offset)), nil
}

func unquoteShell(quoted string) (string, error) {
	var b strings.Builder
	for quoted != "" {
		if !strings.HasPrefix(quoted, "'") {
			if strings.HasPrefix(quoted, `\`) && len(quoted) > 1 {
				b.WriteByte(quoted[1])
				quoted = quoted[2:]
				continue
			}
			return "", fmt.Errorf("%w: author-script value %q", ErrRebaseStateCorrupt, quoted)
		}
		end := strings.IndexByte(quoted[1:], '\'')
		if end < 0 {
			return "", fmt.Errorf("%w: author-script value %q", ErrRebaseStateCorrupt, quoted)
		}
		b.WriteString(quoted[1 : end+1])
		quoted = quoted[end+2:]
	}
	return b.String(), nil
}

func quoteShell(value string) string {
	return "'" + strings.NewReplacer("'", `'\''`, "!", `'\!'`).Replace(value) + "'"
}

func formatAuthorScript(sig object.Signature) string {
	return authorNameKey + "=" + quoteShell(sig.Name) + "\n" +
		authorEmailKey + "=" + quoteShell(sig.Email) + "\n" +
		authorDateKey + "=" + quoteShell("@"+strconv.FormatInt(sig.When.Unix(), 10)+" "+sig.When.Format("-0700")) + "\n"
}

func writeRebaseState(r *repo.Repository, s RebaseState) error {
	if err := fsRootMkdirAll(r.Root(), rebaseDir, 0o777); err != nil {
		return fmt.Errorf("ops: create %s: %w", rebaseDir, err)
	}
	files := []stateFile{
		{rebasePath(rebaseHeadName), s.HeadName + "\n"},
		{rebasePath(rebaseOnto), s.Onto.String() + "\n"},
		{rebasePath(rebaseOrigHead), s.OrigHead.String() + "\n"},
		{rebasePath(rebaseTodo), formatTodo(s.Todo)},
		{rebasePath(rebaseDone), formatTodo(s.Done)},
		{rebasePath(rebaseMsgNum), strconv.Itoa(len(s.Done)) + "\n"},
		{rebasePath(rebaseEnd), strconv.Itoa(len(s.Done)+len(s.Todo)) + "\n"},
		{rebasePath(rebaseRewritten), s.Rewritten},
		{rebasePath(rebaseInteractive), ""},
		{rebasePath(rebaseDropRedundant), ""},
		{rebasePath(rebaseNoReschedule), ""},
	}
	if s.Stopped.IsZero() {
		return errors.Join(writeStateFiles(r, files), removeStateFiles(r, rebasePath(rebaseStopped), rebasePath(rebaseMessage), rebasePath(rebaseAuthorScript), rebaseHeadFile))
	}
	files = append(files,
		stateFile{rebasePath(rebaseStopped), s.Stopped.String() + "\n"},
		stateFile{rebasePath(rebaseMessage), s.Message + "\n"},
		stateFile{rebaseHeadFile, s.Stopped.String() + "\n"},
	)
	if s.Author != nil {
		files = append(files, stateFile{rebasePath(rebaseAuthorScript), formatAuthorScript(*s.Author)})
	}
	return writeStateFiles(r, files)
}

func removeStateFiles(r *repo.Repository, names ...string) error {
	var errs []error
	for _, name := range names {
		if err := fsRootRemove(r.Root(), name); err != nil && !errors.Is(err, fs.ErrNotExist) {
			errs = append(errs, fmt.Errorf("ops: remove %s: %w", name, err))
		}
	}
	return errors.Join(errs...)
}

func clearRebaseState(r *repo.Repository) error {
	if err := removeStateFiles(r, rebaseHeadFile); err != nil {
		return err
	}
	if err := fsRootRemoveAll(r.Root(), rebaseDir); err != nil {
		return fmt.Errorf("ops: remove %s: %w", rebaseDir, err)
	}
	return nil
}
