package cli

import (
	"fmt"
	"io"

	agentspinner "github.com/benoitpetit/agent-spinner"
	"github.com/benoitpetit/voie/internal/app"
)

type progressReporter interface {
	Handle(app.ProgressEvent)
	Finish(error)
}

type lineProgressReporter struct{ output io.Writer }

func (r *lineProgressReporter) Handle(event app.ProgressEvent) {
	_, _ = fmt.Fprintf(r.output, "[%s] %s\n", event.Stage, event.Message)
}
func (*lineProgressReporter) Finish(error) {}

type terminalProgressReporter struct {
	output  io.Writer
	spinner *agentspinner.Instance
}

func (r *terminalProgressReporter) Handle(event app.ProgressEvent) {
	if r.spinner == nil {
		r.spinner = agentspinner.StartCustom(event.Message, agentspinner.Spinner{Frames: []string{"-", "\\", "|", "/"}, Interval: 100}, agentspinner.WithRenderer(agentspinner.NewTerminalRendererWithOutput(r.output)))
		return
	}
	r.spinner.Update(event.Message)
}
func (r *terminalProgressReporter) Finish(err error) {
	if r.spinner == nil {
		return
	}
	if err != nil {
		r.spinner.Fail("Completion failed")
	} else {
		r.spinner.Stop("Completion finished")
	}
}

func newProgressReporter(stderr io.Writer, isTerminal bool) progressReporter {
	if stderr == nil {
		stderr = io.Discard
	}
	if isTerminal {
		return &terminalProgressReporter{output: stderr}
	}
	return &lineProgressReporter{output: stderr}
}
