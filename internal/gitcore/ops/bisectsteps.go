package ops

import (
	"context"
	"errors"
	"slices"
	"strconv"

	"github.com/oops1/gogit/internal/gitcore/commitgraph"
	"github.com/oops1/gogit/internal/gitcore/hash"
	"github.com/oops1/gogit/internal/gitcore/refs"
	"github.com/oops1/gogit/internal/gitcore/repo"
	"github.com/oops1/gogit/internal/gitcore/revision"
)

var bisectSwitch = Switch

type BisectOutcome int

const (
	BisectPending BisectOutcome = iota
	BisectTesting
	BisectMergeBase
	BisectFound
	BisectOnlySkipped
	BisectAmbiguous
)

type BisectCandidate struct {
	Commit  hash.ObjectID
	Subject string
}

type BisectStatus struct {
	Outcome    BisectOutcome
	Start      string
	Current    hash.ObjectID
	Subject    string
	Remaining  int
	Steps      int
	Good       int
	Skipped    int
	HasBad     bool
	Candidates []BisectCandidate
}

type BisectStartOptions struct {
	Bad        string
	Good       []string
	NoCheckout bool
}

func StartBisect(ctx context.Context, r *repo.Repository, opts BisectStartOptions) (BisectStatus, error) {
	if err := ctx.Err(); err != nil {
		return BisectStatus{}, err
	}
	if opts.Bad == "" && len(opts.Good) > 0 {
		return BisectStatus{}, ErrBisectGoodWithoutBad
	}
	if err := restartBisect(ctx, r); err != nil {
		return BisectStatus{}, err
	}
	b, err := newBisector(ctx, r)
	if err != nil {
		return BisectStatus{}, err
	}
	defer b.close()
	if err := b.begin(opts); err != nil {
		return BisectStatus{}, err
	}
	return b.autoNext()
}

func MarkBisect(ctx context.Context, r *repo.Repository, mark BisectMark) (BisectStatus, error) {
	return MarkBisectRevision(ctx, r, mark, "")
}

func MarkBisectRevision(ctx context.Context, r *repo.Repository, mark BisectMark, rev string) (BisectStatus, error) {
	if err := ctx.Err(); err != nil {
		return BisectStatus{}, err
	}
	if _, err := bisectStarted(r); err != nil {
		return BisectStatus{}, err
	}
	b, err := newBisector(ctx, r)
	if err != nil {
		return BisectStatus{}, err
	}
	defer b.close()
	tested, err := b.markedRevision(rev)
	if err != nil {
		return BisectStatus{}, err
	}
	if err := b.setTerms(mark); err != nil {
		return BisectStatus{}, err
	}
	if err := b.writeMark(mark, tested, false); err != nil {
		return BisectStatus{}, err
	}
	return b.autoNext()
}

func ReadBisectStatus(ctx context.Context, r *repo.Repository) (BisectStatus, error) {
	if err := ctx.Err(); err != nil {
		return BisectStatus{}, err
	}
	if _, err := bisectStarted(r); err != nil {
		return BisectStatus{}, err
	}
	b, err := newBisector(ctx, r)
	if err != nil {
		return BisectStatus{}, err
	}
	defer b.close()
	return b.report()
}

func restartBisect(ctx context.Context, r *repo.Repository) error {
	if _, err := bisectStarted(r); err != nil {
		if errors.Is(err, ErrNotBisecting) {
			return nil
		}
		return err
	}
	return ResetBisect(ctx, r)
}

type bisector struct {
	ctx        context.Context
	r          *repo.Repository
	rc         *repoContext
	graph      *commitgraph.Graph
	marks      bisectMarks
	start      string
	noCheckout bool
}

func newBisector(ctx context.Context, r *repo.Repository) (*bisector, error) {
	rc, err := openRepoContext(r)
	if err != nil {
		return nil, err
	}
	graph, err := OpenCommitGraph(r, rc.db)
	if err != nil {
		_ = rc.close()
		return nil, err
	}
	b := &bisector{ctx: ctx, r: r, rc: rc, graph: graph}
	if err := b.reload(); err != nil {
		_ = rc.close()
		return nil, err
	}
	return b, nil
}

func (b *bisector) close() { _ = b.rc.close() }

func (b *bisector) reload() error {
	marks, err := readBisectMarks(b.rc.refs)
	if err != nil {
		return err
	}
	start, err := readStateFile(b.r, bisectStartFile)
	if err != nil {
		return err
	}
	noCheckout, err := stateFileExists(b.r, bisectHeadFile)
	if err != nil {
		return err
	}
	b.marks, b.start, b.noCheckout = marks, bisectOrigin(start), noCheckout
	return nil
}

func (b *bisector) revisions() revision.Context {
	return revision.Context{Objects: mergeStore{db: b.rc.db}, Graph: b.graph}
}

func (b *bisector) begin(opts BisectStartOptions) error {
	headRef, headCommit, err := currentHeadState(b.rc.refs)
	if err != nil {
		return err
	}
	origin := headCommit.String()
	if headRef != "" {
		origin = headRef.Short()
	}
	args, marked, err := b.resolveStartRevisions(opts)
	if err != nil {
		return err
	}
	if err := clearBisectStateWith(b.rc); err != nil {
		return err
	}
	if err := writeStateFile(b.r, bisectStartFile, origin+"\n"); err != nil {
		return err
	}
	if opts.NoCheckout {
		if err := writeStateFile(b.r, bisectHeadFile, headCommit.String()+"\n"); err != nil {
			return err
		}
		b.noCheckout = true
	}
	if err := writeStateFile(b.r, bisectNamesFile, "\n"); err != nil {
		return err
	}
	for _, m := range marked {
		if err := b.writeMark(m.mark, m.id, true); err != nil {
			return err
		}
	}
	if len(marked) > 0 {
		if err := writeStateFile(b.r, bisectTermsFile, bisectTermBad+"\n"+bisectTermGood+"\n"); err != nil {
			return err
		}
	}
	b.start = origin
	return appendStateFile(b.r, bisectLogFile, "git bisect start"+quoteBisectArgs(args)+"\n")
}

type bisectMarked struct {
	mark BisectMark
	id   hash.ObjectID
}

func (b *bisector) resolveStartRevisions(opts BisectStartOptions) ([]string, []bisectMarked, error) {
	var args []string
	if opts.NoCheckout {
		args = append(args, "--no-checkout")
	}
	var marked []bisectMarked
	if opts.Bad != "" {
		id, err := resolveCommittish(b.rc, opts.Bad)
		if err != nil {
			return nil, nil, err
		}
		args = append(args, opts.Bad)
		marked = append(marked, bisectMarked{mark: BisectBad, id: id})
	}
	for _, rev := range opts.Good {
		id, err := resolveCommittish(b.rc, rev)
		if err != nil {
			return nil, nil, err
		}
		args = append(args, rev)
		marked = append(marked, bisectMarked{mark: BisectGood, id: id})
	}
	return args, marked, nil
}

func (b *bisector) setTerms(mark BisectMark) error {
	if mark == BisectSkip {
		return nil
	}
	text, err := readStateFile(b.r, bisectTermsFile)
	if err != nil || text != "" {
		return err
	}
	return writeStateFile(b.r, bisectTermsFile, bisectTermBad+"\n"+bisectTermGood+"\n")
}

func (b *bisector) markedRevision(rev string) (hash.ObjectID, error) {
	switch {
	case rev != "":
		return resolveCommittish(b.rc, rev)
	case b.noCheckout:
		return readHeadFile(b.r, bisectHeadFile)
	}
	_, id, err := currentHeadState(b.rc.refs)
	return id, err
}

func (b *bisector) writeMark(mark BisectMark, id hash.ObjectID, quiet bool) error {
	tx := b.rc.refs.Begin()
	if err := txSet(tx, bisectRefOf(mark, id), id); err != nil {
		tx.Rollback()
		return err
	}
	if err := txCommit(tx); err != nil {
		return err
	}
	subject, err := b.subjectOf(id)
	if err != nil {
		return err
	}
	line := "# " + string(mark) + ": [" + id.String() + "] " + subject + "\n"
	if !quiet {
		line += "git bisect " + string(mark) + " " + id.String() + "\n"
	}
	return appendStateFile(b.r, bisectLogFile, line)
}

func (b *bisector) subjectOf(id hash.ObjectID) (string, error) {
	commit, err := dbCommit(b.rc.db, id)
	if err != nil {
		return "", err
	}
	return stashSubject(commit.Message), nil
}

func (b *bisector) autoNext() (BisectStatus, error) {
	if err := b.reload(); err != nil {
		return BisectStatus{}, err
	}
	if !b.marks.ready() {
		if err := appendStateFile(b.r, bisectLogFile, "# "+b.waitingLine()+"\n"); err != nil {
			return BisectStatus{}, err
		}
		return b.status(BisectPending, nil), nil
	}
	return b.next()
}

func (b *bisector) waitingLine() string {
	switch {
	case b.marks.hasBad:
		return "status: waiting for good commit(s), bad commit known"
	case len(b.marks.good) == 1:
		return "status: waiting for bad commit, 1 good commit known"
	case len(b.marks.good) > 1:
		return "status: waiting for bad commit, " + strconv.Itoa(len(b.marks.good)) + " good commits known"
	}
	return "status: waiting for both good and bad commits"
}

func (b *bisector) status(outcome BisectOutcome, chosen *bisectNode) BisectStatus {
	status := BisectStatus{
		Outcome: outcome,
		Start:   b.start,
		Current: b.marks.bad,
		Good:    len(b.marks.good),
		Skipped: len(b.marks.skipped),
		HasBad:  b.marks.hasBad,
	}
	if chosen != nil {
		status.Current, status.Subject = chosen.id, chosen.subject
	}
	return status
}

func (b *bisector) next() (BisectStatus, error) {
	checked, err := ancestorsAlreadyChecked(b.r)
	if err != nil {
		return BisectStatus{}, err
	}
	if !checked {
		status, done, err := b.testMergeBases()
		if err != nil || done {
			return status, err
		}
		if err := markAncestorsChecked(b.r); err != nil {
			return BisectStatus{}, err
		}
	}
	plan, err := b.plan()
	if err != nil {
		return BisectStatus{}, err
	}
	switch plan.outcome(b.marks.bad) {
	case BisectAmbiguous:
		return b.status(BisectAmbiguous, nil), nil
	case BisectOnlySkipped:
		return b.reportOnlySkipped(plan)
	case BisectFound:
		return b.reportFound(plan)
	}
	status := b.status(BisectTesting, plan.head)
	status.Remaining, status.Steps = plan.remaining(), estimateBisectSteps(plan.all())
	if err := b.checkout(plan.head.id); err != nil {
		return BisectStatus{}, err
	}
	return status, nil
}

func (b *bisector) report() (BisectStatus, error) {
	if !b.marks.ready() {
		return b.status(BisectPending, nil), nil
	}
	checked, err := ancestorsAlreadyChecked(b.r)
	if err != nil {
		return BisectStatus{}, err
	}
	if !checked {
		expected, err := readHeadFile(b.r, bisectExpectedRevFile)
		if err != nil {
			return BisectStatus{}, err
		}
		status := b.status(BisectMergeBase, nil)
		status.Current = expected
		status.Subject, err = b.subjectOf(expected)
		return status, err
	}
	plan, err := b.plan()
	if err != nil {
		return BisectStatus{}, err
	}
	outcome := plan.outcome(b.marks.bad)
	status := b.status(outcome, plan.head)
	if outcome == BisectTesting {
		status.Remaining, status.Steps = plan.remaining(), estimateBisectSteps(plan.all())
	}
	if outcome == BisectOnlySkipped {
		status.Current, status.Candidates = b.marks.bad, plan.candidates()
	}
	return status, nil
}

func (b *bisector) reportOnlySkipped(plan bisectPlan) (BisectStatus, error) {
	status := b.status(BisectOnlySkipped, nil)
	status.Candidates = plan.candidates()
	text := "# only skipped commits left to test\n"
	for _, candidate := range status.Candidates {
		text += "# possible first bad commit: [" + candidate.Commit.String() + "] " + candidate.Subject + "\n"
	}
	return status, appendStateFile(b.r, bisectLogFile, text)
}

func (b *bisector) reportFound(plan bisectPlan) (BisectStatus, error) {
	status := b.status(BisectFound, plan.head)
	text := "# first " + bisectTermBad + " commit: [" + status.Current.String() + "] " + status.Subject + "\n"
	return status, appendStateFile(b.r, bisectLogFile, text)
}

func (b *bisector) testMergeBases() (BisectStatus, bool, error) {
	for _, good := range b.marks.good {
		bases, err := revision.MergeBase(b.revisions(), b.marks.bad, good)
		if err != nil {
			return BisectStatus{}, true, err
		}
		for _, base := range bases {
			if b.marks.isGood(base) || b.marks.isSkipped(base) {
				continue
			}
			if base == b.marks.bad {
				return BisectStatus{}, true, b.badMergeBase()
			}
			status, err := b.checkoutMergeBase(base)
			return status, true, err
		}
	}
	return BisectStatus{}, false, nil
}

func (b *bisector) badMergeBase() error {
	expected, err := readHeadFile(b.r, bisectExpectedRevFile)
	if err != nil {
		return err
	}
	if expected == b.marks.bad {
		return ErrBisectMergeBaseBad
	}
	return ErrBisectGoodNotAncestor
}

func (b *bisector) checkoutMergeBase(base hash.ObjectID) (BisectStatus, error) {
	subject, err := b.subjectOf(base)
	if err != nil {
		return BisectStatus{}, err
	}
	status := b.status(BisectMergeBase, &bisectNode{id: base, subject: subject})
	return status, b.checkout(base)
}

func (b *bisector) checkout(id hash.ObjectID) error {
	if err := writeStateFile(b.r, bisectExpectedRevFile, id.String()+"\n"); err != nil {
		return err
	}
	if b.noCheckout {
		return writeStateFile(b.r, bisectHeadFile, id.String()+"\n")
	}
	return bisectSwitch(b.ctx, b.r, id.String(), SwitchOptions{})
}

type bisectPlan struct {
	list    []*bisectNode
	head    *bisectNode
	tried   []*bisectNode
	reaches int
}

func (p bisectPlan) all() int { return len(p.list) }

func (p bisectPlan) remaining() int { return len(p.list) - p.reaches - 1 }

func (p bisectPlan) outcome(bad hash.ObjectID) BisectOutcome {
	switch {
	case p.head == nil && len(p.tried) > 0, p.head != nil && p.head.id == bad && len(p.tried) > 0:
		return BisectOnlySkipped
	case p.head == nil:
		return BisectAmbiguous
	case p.head.id == bad:
		return BisectFound
	}
	return BisectTesting
}

func (p bisectPlan) candidates() []BisectCandidate {
	candidates := make([]BisectCandidate, 0, len(p.list))
	for _, n := range slices.Backward(p.list) {
		candidates = append(candidates, BisectCandidate{Commit: n.id, Subject: n.subject})
	}
	return candidates
}

func (b *bisector) plan() (bisectPlan, error) {
	list, err := b.candidateCommits()
	if err != nil || len(list) == 0 {
		return bisectPlan{}, err
	}
	ordered, reaches := findBisection(list, len(b.marks.skipped) > 0)
	plan := bisectPlan{list: list, head: ordered[0], reaches: reaches}
	if len(b.marks.skipped) == 0 {
		return plan, nil
	}
	kept, tried, firstSkipped := b.marks.filterSkipped(ordered)
	plan.tried = tried
	if firstSkipped && len(kept) > 0 {
		kept = skipBisectionAway(kept, b.marks.bad)
	}
	plan.head = nil
	if len(kept) > 0 {
		plan.head = kept[0]
	}
	return plan, nil
}

func (m bisectMarks) filterSkipped(list []*bisectNode) (kept, tried []*bisectNode, firstSkipped bool) {
	for at, n := range list {
		if !m.isSkipped(n.id) {
			kept = append(kept, n)
			continue
		}
		firstSkipped = firstSkipped || at == 0
		tried = append(tried, n)
	}
	return kept, tried, firstSkipped
}

func (b *bisector) candidateCommits() ([]*bisectNode, error) {
	walk := revision.Walk(b.ctx, revision.Options{
		Context: b.revisions(),
		Include: []hash.ObjectID{b.marks.bad},
		Exclude: b.marks.good,
	})
	var list []*bisectNode
	for commit, err := range walk {
		if err != nil {
			return nil, err
		}
		list = append(list, &bisectNode{id: commit.ID, subject: stashSubject(commit.Message), parents: commit.Parents})
	}
	slices.Reverse(list)
	return list, nil
}

func clearBisectStateWith(rc *repoContext) error {
	tx := rc.refs.Begin()
	for ref, err := range rc.refs.Prefix(refs.BisectPrefix) {
		if err == nil {
			err = txDelete(tx, ref.Name, ref.Target)
		}
		if err != nil {
			tx.Rollback()
			return err
		}
	}
	if err := txCommit(tx); err != nil {
		return err
	}
	if err := removeStateFiles(rc.repo, bisectStateFiles...); err != nil {
		return err
	}
	return removeStateFiles(rc.repo, bisectStartFile)
}
