package progress

const (
	PhaseCounting    = "counting"
	PhaseCompressing = "compressing"
	PhaseWriting     = "writing"
	PhaseReceiving   = "receiving"
	PhaseResolving   = "resolving"
	PhaseCheckout    = "checkout"
)

type Report struct {
	Phase   string
	Current int64
	Total   int64
	Message string
}

type Func func(Report)

func (f Func) Report(report Report) {
	if f == nil {
		return
	}
	f(report)
}

func (f Func) Phase(name string) {
	f.Report(Report{Phase: name})
}

func (f Func) Message(text string) {
	f.Report(Report{Message: text})
}

func (f Func) Count(phase string, current, total int64) {
	f.Report(Report{Phase: phase, Current: current, Total: total})
}
