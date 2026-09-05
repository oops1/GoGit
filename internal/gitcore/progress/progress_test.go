package progress

import (
	"slices"
	"testing"
)

func collect() (Func, *[]Report) {
	reports := new([]Report)
	return func(report Report) { *reports = append(*reports, report) }, reports
}

func TestNilFuncIgnoresEveryCall(t *testing.T) {
	var sink Func
	sink.Report(Report{Phase: PhaseWriting})
	sink.Phase(PhaseCounting)
	sink.Message("text")
	sink.Count(PhaseReceiving, 1, 2)
}

func TestReportPassesTheValueThrough(t *testing.T) {
	sink, reports := collect()
	want := Report{Phase: PhaseResolving, Current: 3, Total: 7, Message: "text"}
	sink.Report(want)
	if !slices.Equal(*reports, []Report{want}) {
		t.Fatalf("reports = %v, want %v", *reports, []Report{want})
	}
}

func TestPhaseMessageAndCountFillOnlyTheirFields(t *testing.T) {
	sink, reports := collect()
	sink.Phase(PhaseCompressing)
	sink.Message("done")
	sink.Count(PhaseCheckout, 4, 9)
	want := []Report{
		{Phase: PhaseCompressing},
		{Message: "done"},
		{Phase: PhaseCheckout, Current: 4, Total: 9},
	}
	if !slices.Equal(*reports, want) {
		t.Fatalf("reports = %v, want %v", *reports, want)
	}
}
