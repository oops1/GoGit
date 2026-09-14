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
	rebaseApplyDir      = "rebase-apply"
	todoNoop            = "noop"
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
	rebaseAmend         = "amend"
	actionPick          = "pick"
	actionReword        = "reword"
	actionEdit          = "edit"
	actionSquash        = "squash"
	actionFixup         = "fixup"
	actionDrop          = "drop"
	authorNameKey       = "GIT_AUTHOR_NAME"
	authorEmailKey      = "GIT_AUTHOR_EMAIL"
	authorDateKey       = "GIT_AUTHOR_DATE"
)

var (
	ErrRebaseStateCorrupt    = errors.New("ops: the rebase state cannot be read")
	ErrRebaseApplyInProgress = errors.New("ops: git am or git rebase --apply is in progress")
)

type RebaseStep struct {
	Action  string
	Commit  hash.ObjectID
	Subject string
	Line    string
}

func (s RebaseStep) line() string {
	if s.Line != "" {
		return s.Line + "\n"
	}
	return s.Action + " " + s.Commit.String() + " " + s.Subject + "\n"
}

var todoCommands = map[string]string{
	actionPick: actionPick, "p": actionPick,
	actionReword: actionReword, "r": actionReword,
	actionEdit: actionEdit, "e": actionEdit,
	actionSquash: actionSquash, "s": actionSquash,
	actionFixup: actionFixup, "f": actionFixup,
	actionDrop: actionDrop, "d": actionDrop,
}

type RebaseState struct {
	HeadName  string
	Onto      hash.ObjectID
	OrigHead  hash.ObjectID
	Done      []RebaseStep
	Todo      []RebaseStep
	Stopped   hash.ObjectID
	Amend     hash.ObjectID
	Message   string
	Author    *object.Signature
	Rewritten string
	Applying  bool
}

func (s RebaseState) Amending() bool { return !s.Amend.IsZero() }

func (s RebaseState) InProgress() bool { return s.HeadName != "" || s.Applying }

func rebasePath(name string) string { return path.Join(rebaseDir, name) }

func rebaseApplyInProgress(r *repo.Repository) bool {
	info, err := fsRootLstat(r.Root(), rebaseApplyDir)
	return err == nil && info.IsDir()
}

func readRebaseHead(r *repo.Repository) (RebaseState, error) {
	headName, err := readStateFile(r, rebasePath(rebaseHeadName))
	if err != nil {
		return RebaseState{}, err
	}
	state := RebaseState{HeadName: strings.TrimSpace(headName)}
	origHead := rebasePath(rebaseOrigHead)
	if state.HeadName == "" {
		if !rebaseApplyInProgress(r) {
			return RebaseState{}, nil
		}
		if headName, err = readStateFile(r, path.Join(rebaseApplyDir, rebaseHeadName)); err != nil {
			return RebaseState{}, err
		}
		state.HeadName, state.Applying = strings.TrimSpace(headName), true
		origHead = path.Join(rebaseApplyDir, rebaseOrigHead)
		if state.HeadName == "" {
			origHead = origHeadFile
		}
	}
	if state.OrigHead, err = readHeadFile(r, origHead); err != nil {
		return RebaseState{}, err
	}
	return state, nil
}

func ReadRebaseState(r *repo.Repository) (RebaseState, error) {
	state, err := readRebaseHead(r)
	if err != nil || state.Applying || !state.InProgress() {
		return state, err
	}
	for name, into := range map[string]*hash.ObjectID{rebaseOnto: &state.Onto, rebaseStopped: &state.Stopped, rebaseAmend: &state.Amend} {
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
	state.Message = trimMessage(normalizeMessage(message))
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
		if line == "" || strings.HasPrefix(line, "#") || line == todoNoop {
			continue
		}
		step, err := parseTodoLine(line)
		if err != nil {
			return nil, err
		}
		steps = append(steps, step)
	}
	return steps, nil
}

func parseTodoLine(line string) (RebaseStep, error) {
	fields := strings.SplitN(line, " ", 3)
	action, known := todoCommands[fields[0]]
	if !known || len(fields) > 1 && strings.HasPrefix(fields[1], "-") {
		return RebaseStep{Action: fields[0], Line: line}, nil
	}
	if len(fields) < 2 {
		return RebaseStep{}, fmt.Errorf("%w: %q", ErrRebaseStateCorrupt, line)
	}
	id, err := hash.Parse(fields[1])
	if err != nil {
		return RebaseStep{}, fmt.Errorf("%w: %q: %w", ErrRebaseStateCorrupt, line, err)
	}
	step := RebaseStep{Action: action, Commit: id}
	if len(fields) == 3 {
		step.Subject = fields[2]
	}
	return step, nil
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
	if err := ensureDirectories(r.Root(), rebaseDir); err != nil {
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
		return errors.Join(writeStateFiles(r, files), removeStateFiles(r, rebasePath(rebaseStopped), rebasePath(rebaseMessage), rebasePath(rebaseAuthorScript), rebasePath(rebaseAmend), rebaseHeadFile))
	}
	files = append(files,
		stateFile{rebasePath(rebaseStopped), s.Stopped.String() + "\n"},
		stateFile{rebasePath(rebaseMessage), s.Message + "\n"},
		stateFile{rebaseHeadFile, s.Stopped.String() + "\n"},
	)
	if s.Author != nil {
		files = append(files, stateFile{rebasePath(rebaseAuthorScript), formatAuthorScript(*s.Author)})
	}
	if !s.Amending() {
		return errors.Join(writeStateFiles(r, files), removeStateFiles(r, rebasePath(rebaseAmend)))
	}
	return writeStateFiles(r, append(files, stateFile{rebasePath(rebaseAmend), s.Amend.String() + "\n"}))
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
	for _, dir := range []string{rebaseDir, rebaseApplyDir} {
		if err := fsRootRemoveAll(r.Root(), dir); err != nil {
			return fmt.Errorf("ops: remove %s: %w", dir, err)
		}
	}
	return nil
}
